package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/gofrs/flock"

	"cvtranslate/internal/desktopworkflow"
	"cvtranslate/internal/domain"
	"cvtranslate/internal/platform"
	"cvtranslate/internal/server"
	"cvtranslate/internal/speech"
	"cvtranslate/internal/storage"
	"cvtranslate/internal/translation"
	"cvtranslate/internal/update"
	"cvtranslate/web"
)

var version = "dev"
var commit = "unavailable"
var buildTime = "unavailable"

type arguments struct {
	dataDirectory string
	port          int
	viteURL       string
	noOpen        bool
	noNative      bool
}

type instanceInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func main() {
	var options arguments
	showVersion := flag.Bool("version", false, "Print version without initializing the application")
	flag.StringVar(&options.dataDirectory, "data-dir", "", "User data directory (isolated in development)")
	flag.IntVar(&options.port, "port", 0, "Loopback HTTP port; 0 uses saved settings")
	flag.StringVar(&options.viteURL, "vite-url", "http://127.0.0.1:19329", "Development frontend URL (dev build only)")
	flag.BoolVar(&options.noOpen, "no-open", false, "Do not open the browser automatically")
	flag.BoolVar(&options.noNative, "no-native", false, "Disable native desktop integration for headless checks")
	flag.Parse()
	if *showVersion {
		fmt.Printf("CVTranslate %s (%s/%s) commit=%s built=%s\n", version, runtime.GOOS, runtime.GOARCH, commit, buildTime)
		return
	}
	run := func() {
		if err := runApplication(options); err != nil {
			log.Printf("CVTranslate stopped: %v", err)
			os.Exit(1)
		}
	}
	if options.noNative {
		run()
	} else {
		platform.RunMain(run)
	}
}

func resolveDataDirectory(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if web.Development {
		return filepath.Abs(filepath.Join(".local", "dev"))
	}
	configurationDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New("cannot locate the user configuration directory")
	}
	return filepath.Join(configurationDirectory, "CVTranslate"), nil
}

func runApplication(options arguments) (resultError error) {
	dataDirectory, err := resolveDataDirectory(options.dataDirectory)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDirectory, 0700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	if err := restrictDirectory(dataDirectory); err != nil {
		return fmt.Errorf("protect data directory: %w", err)
	}
	instanceLock := flock.New(filepath.Join(dataDirectory, "instance.lock"))
	locked, err := instanceLock.TryLock()
	if err != nil {
		return errors.New("cannot acquire the application instance lock")
	}
	if !locked {
		if options.noOpen {
			return errors.New("an instance is already using this data directory")
		}
		return reopenExisting(dataDirectory)
	}
	defer func() { _ = instanceLock.Close() }()
	logFile, err := os.OpenFile(filepath.Join(dataDirectory, "application.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("cannot open diagnostic log")
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stderr, logFile))
	defer log.SetOutput(os.Stderr)
	defer func() {
		if resultError != nil {
			log.Printf("Application error: %v", resultError)
		}
	}()
	repository, err := storage.Open(filepath.Join(dataDirectory, "cvtranslate.db"))
	if err != nil {
		return errors.New("cannot initialize local database; existing data was not removed")
	}
	defer repository.Close()
	settings, err := repository.GetSettings(context.Background())
	if err != nil {
		return errors.New("cannot load application settings")
	}
	port := options.port
	if port == 0 {
		port = settings.Port
		if web.Development {
			port = 19328
		}
	}
	if port < 1024 || port > 65535 {
		return errors.New("port must be between 1024 and 65535")
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return fmt.Errorf("cannot listen on %s; check port conflicts", address)
	}
	defer listener.Close()
	assets, err := web.Handler(options.viteURL)
	if err != nil {
		return err
	}
	credentialNamespace := "cvtranslate.production"
	if web.Development {
		credentialNamespace = "cvtranslate.development"
	}
	credentials := platform.NewCredentials(credentialNamespace)
	jobs := translation.New(repository, credentials)
	defer jobs.Close()
	applicationContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	controlToken := domain.NewID() + domain.NewID()
	serverOptions := server.Options{Origin: "http://" + address, Version: version, Development: web.Development, Assets: assets, Credentials: credentials, Speech: speech.New(), Updates: update.New(), OpenBrowser: platform.OpenBrowser, ControlToken: controlToken}
	var native *platform.Desktop
	var workflow *desktopworkflow.Controller
	if !options.noNative {
		native = platform.NewDesktop()
		defer native.Close()
		serverOptions.ConfigureHotkeys = func(keys domain.Hotkeys) error { return native.ConfigureHotkeys(keys, workflow.Handle) }
		serverOptions.PlatformStatus = func() any {
			status := native.Status()
			var shortcut desktopworkflow.ShortcutStatus
			if workflow != nil {
				shortcut = workflow.Status()
			}
			return struct {
				platform.Status
				Shortcut desktopworkflow.ShortcutStatus `json:"shortcut"`
			}{Status: status, Shortcut: shortcut}
		}
	} else {
		serverOptions.PlatformStatus = func() any {
			return map[string]any{"os": runtime.GOOS, "hotkeysAvailable": false, "pasteAvailable": false, "message": "Native integration disabled by --no-native."}
		}
	}
	application := server.New(repository, jobs, serverOptions)
	if native != nil {
		workflow = desktopworkflow.New(native, jobs, repository, application.StartInput, application.OpenUI)
		defer workflow.Close()
		if err := native.ConfigureHotkeys(settings.Hotkeys, workflow.Handle); err != nil {
			log.Print("Global shortcut registration failed; review Settings and OS permissions.")
		}
	}
	instancePath := filepath.Join(dataDirectory, "instance.json")
	instanceBytes, err := json.Marshal(instanceInfo{Port: port, Token: controlToken})
	if err != nil {
		return errors.New("cannot create instance metadata")
	}
	if err := os.WriteFile(instancePath, instanceBytes, 0600); err != nil {
		return errors.New("cannot save protected instance metadata")
	}
	defer os.Remove(instancePath)
	var trayEvents <-chan platform.TrayEvent
	var tray *platform.Tray
	if native != nil {
		tray, err = platform.StartTray()
		if err != nil {
			return fmt.Errorf("cannot start system tray: %w", err)
		}
		trayEvents = tray.Events
		defer tray.Close()
	}
	httpServer := &http.Server{Handler: application, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: time.Minute, MaxHeaderBytes: 16 * 1024, BaseContext: func(net.Listener) context.Context { return applicationContext }}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- httpServer.Serve(listener) }()
	stopReopen := platform.OnReopen(func() {
		if err := application.OpenUI(""); err != nil {
			log.Print("Could not reopen the browser from the application reopen event.")
		}
	})
	defer stopReopen()
	log.Printf("CVTranslate ready at http://%s (development=%t)", address, web.Development)
	resultError = waitForShutdown(applicationContext, stop, serverErrors, trayEvents, !options.noOpen, func() error {
		return application.OpenUI("")
	})
	// Invalidate pending paste work before waiting for HTTP requests or providers.
	if workflow != nil {
		workflow.Close()
	}
	if native != nil {
		resultError = errors.Join(resultError, native.Close())
	}
	stopReopen()
	if tray != nil {
		resultError = errors.Join(resultError, tray.Close())
	}
	resultError = errors.Join(resultError, jobs.Close())
	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		_ = httpServer.Close()
	}
	return resultError
}

func reopenExisting(dataDirectory string) error {
	metadata, err := os.ReadFile(filepath.Join(dataDirectory, "instance.json"))
	if err != nil {
		return errors.New("another instance is starting; try again shortly")
	}
	var instance instanceInfo
	if json.Unmarshal(metadata, &instance) != nil || instance.Port < 1024 || instance.Port > 65535 || len(instance.Token) != 64 {
		return errors.New("existing instance metadata is invalid")
	}
	request, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+strconv.Itoa(instance.Port)+"/internal/open", nil)
	if err != nil {
		return errors.New("cannot prepare local reopen request")
	}
	request.Header.Set("X-Instance-Token", instance.Token)
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("cannot contact the existing instance")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("existing instance could not open its browser")
	}
	return nil
}
