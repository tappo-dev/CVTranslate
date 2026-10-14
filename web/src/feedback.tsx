import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import * as AlertDialog from "@radix-ui/react-alert-dialog";
import * as Toast from "@radix-ui/react-toast";
import { Check, CircleAlert, X } from "lucide-react";

interface Confirmation {
  title: string;
  description: string;
  confirmLabel?: string;
  danger?: boolean;
}

interface Feedback {
  notify: (message: string, error?: boolean) => void;
  confirm: (confirmation: Confirmation) => Promise<boolean>;
}

const FeedbackContext = createContext<Feedback | null>(null);

export function useFeedback() {
  const feedback = useContext(FeedbackContext);
  if (!feedback) throw new Error("FeedbackProvider is required");
  return feedback;
}

export function FeedbackProvider({ children }: { children: ReactNode }) {
  const [notifications, setNotifications] = useState<
    { id: number; message: string; error: boolean }[]
  >([]);
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null);
  const nextId = useRef(0);
  const pending = useRef<((confirmed: boolean) => void) | null>(null);
  const returnFocus = useRef<HTMLElement | null>(null);
  const notify = useCallback((message: string, error = false) => {
    const id = ++nextId.current;
    setNotifications((current) => [
      ...current.slice(-3),
      { id, message, error },
    ]);
  }, []);
  const confirm = useCallback((options: Confirmation) => {
    if (pending.current) return Promise.resolve(false);
    returnFocus.current =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    setConfirmation(options);
    return new Promise<boolean>((resolve) => {
      pending.current = resolve;
    });
  }, []);
  const resolveConfirmation = useCallback((confirmed: boolean) => {
    pending.current?.(confirmed);
    pending.current = null;
    setConfirmation(null);
  }, []);
  useEffect(
    () => () => {
      pending.current?.(false);
    },
    [],
  );

  return (
    <FeedbackContext.Provider value={{ notify, confirm }}>
      <Toast.Provider duration={4000} swipeDirection="right" label="通知">
        {children}
        {notifications.map((notification) => (
          <Toast.Root
            key={notification.id}
            className={`toast ${notification.error ? "error" : ""}`}
            duration={notification.error ? 7000 : 4000}
            onOpenChange={(open) => {
              if (!open)
                setNotifications((current) =>
                  current.filter((item) => item.id !== notification.id),
                );
            }}
          >
            {notification.error ? (
              <CircleAlert size={18} />
            ) : (
              <Check size={18} />
            )}
            <Toast.Description>{notification.message}</Toast.Description>
            <Toast.Close className="icon-button" aria-label="关闭通知">
              <X size={16} />
            </Toast.Close>
          </Toast.Root>
        ))}
        <Toast.Viewport className="toast-viewport" label="通知（F8）" />
      </Toast.Provider>
      <AlertDialog.Root
        open={confirmation !== null}
        onOpenChange={(open) => {
          if (!open) resolveConfirmation(false);
        }}
      >
        <AlertDialog.Portal>
          <AlertDialog.Overlay className="dialog-overlay" />
          <AlertDialog.Content
            className="confirmation-dialog"
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              if (returnFocus.current?.isConnected) returnFocus.current.focus();
            }}
          >
            <AlertDialog.Title>{confirmation?.title}</AlertDialog.Title>
            <AlertDialog.Description>
              {confirmation?.description}
            </AlertDialog.Description>
            <div className="button-row">
              <AlertDialog.Cancel asChild>
                <button
                  type="button"
                  onClick={() => resolveConfirmation(false)}
                >
                  取消
                </button>
              </AlertDialog.Cancel>
              <AlertDialog.Action asChild>
                <button
                  type="button"
                  className={confirmation?.danger ? "danger" : "primary"}
                  onClick={() => resolveConfirmation(true)}
                >
                  {confirmation?.confirmLabel || "确认"}
                </button>
              </AlertDialog.Action>
            </div>
          </AlertDialog.Content>
        </AlertDialog.Portal>
      </AlertDialog.Root>
    </FeedbackContext.Provider>
  );
}
