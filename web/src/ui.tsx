import { useEffect, useRef, useState, type ReactNode } from "react";
import { Copy, LoaderCircle, Square, Volume2 } from "lucide-react";
import { errorMessage } from "./api";
import { useFeedback } from "./feedback";
import { statusLabels, type Task } from "./types";

export function Notice({
  children,
  error = false,
}: {
  children: ReactNode;
  error?: boolean;
}) {
  return children ? (
    <div
      className={`notice ${error ? "error" : ""}`}
      role={error ? "alert" : "status"}
    >
      {children}
    </div>
  ) : null;
}
export function Empty({
  title,
  children,
}: {
  title: string;
  children?: ReactNode;
}) {
  return (
    <div className="empty">
      <div className="empty-symbol">文</div>
      <h3>{title}</h3>
      {children && <p>{children}</p>}
    </div>
  );
}
export function Loading() {
  return (
    <div className="loading" role="status">
      <LoaderCircle className="spin" size={19} /> 正在加载…
    </div>
  );
}
export function Status({ task }: { task: Task }) {
  return (
    <span className={`badge ${task.status}`}>
      {task.status === "succeeded" && task.reusedFromTaskId
        ? "已复用"
        : statusLabels[task.status]}
    </span>
  );
}
export function CopyButton({ text }: { text: string }) {
  const { notify } = useFeedback();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return (
    <span className="copy-control">
      <button
        type="button"
        disabled={!text}
        onClick={() => {
          if (!navigator.clipboard) {
            notify("无法访问剪贴板，请手动复制。", true);
            return;
          }
          void navigator.clipboard
            .writeText(text)
            .then(() => {
              if (mounted.current) notify("已复制");
            })
            .catch(() => {
              if (mounted.current) notify("复制失败，请手动复制。", true);
            });
        }}
      >
        <Copy size={15} />
        复制
      </button>
    </span>
  );
}

export function CopyIconButton({
  text,
  label,
}: {
  text: string;
  label: string;
}) {
  const { notify } = useFeedback();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return (
    <button
      type="button"
      className="icon-button history-copy-button"
      aria-label={label}
      title={label}
      disabled={!text}
      onClick={() => {
        if (!navigator.clipboard) {
          notify("无法访问剪贴板，请手动复制。", true);
          return;
        }
        void navigator.clipboard
          .writeText(text)
          .then(() => {
            if (mounted.current) notify("已复制");
          })
          .catch(() => {
            if (mounted.current) notify("复制失败，请手动复制。", true);
          });
      }}
    >
      <Copy size={14} />
    </button>
  );
}

export function SpeakButton({
  text,
  speaking,
  loading,
  onToggle,
}: {
  text: string;
  speaking: boolean;
  loading: boolean;
  onToggle: () => void;
}) {
  const tooLong = Array.from(text).length > 20000;
  const label = loading ? "取消加载" : speaking ? "停止朗读" : "朗读";
  return (
    <button
      type="button"
      className={`speech-button ${speaking ? "speaking" : ""}`}
      aria-pressed={speaking}
      aria-label={label}
      title={
        tooLong && !speaking
          ? "每次朗读最多 20,000 字符"
          : speaking
            ? "停止朗读"
            : "朗读文本"
      }
      disabled={!speaking && (!text.trim() || tooLong)}
      onClick={onToggle}
    >
      {loading ? (
        <LoaderCircle className="spin" size={16} />
      ) : speaking ? (
        <Square size={13} />
      ) : (
        <Volume2 size={16} />
      )}
      {label}
    </button>
  );
}

// Mutations are never retried automatically. Abort and generation guards prevent stale UI updates.
export function useAction() {
  const { notify } = useFeedback();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const controller = useRef<AbortController | null>(null);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      controller.current?.abort();
    };
  }, []);
  async function run(
    operation: (signal: AbortSignal) => Promise<void>,
    success = "",
  ) {
    if (!mounted.current || controller.current) return;
    const current = new AbortController();
    controller.current = current;
    setBusy(true);
    setError("");
    try {
      await operation(current.signal);
      if (mounted.current && !current.signal.aborted && success)
        notify(success);
    } catch (caught) {
      if (mounted.current && !current.signal.aborted)
        setError(errorMessage(caught));
    } finally {
      controller.current = null;
      if (mounted.current) setBusy(false);
    }
  }
  return { busy, error, run, setError };
}
