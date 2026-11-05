// The admin page's root: ask the hub where we stand (password set? signed in?) and show setup, sign-in or the shell.
import { t } from "../i18n";
import { useCallback, useEffect, useState } from "react";
import { Button, Spinner, Toasts } from "../ui/atoms";
import { adminApi, errorText, handleLoginRequired, setCsrf } from "./api";
import { Login, Setup } from "./pages/Auth";
import { Shell } from "./Shell";
import type { AdminState } from "./types";

export function AdminApp() {
  const [state, setState] = useState<AdminState | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = useCallback(() => {
    setError("");
    adminApi<AdminState>("/state").then(
      (s) => {
        setCsrf(s.csrf);
        setState(s);
      },
      (e) => setError(errorText(e)),
    );
  }, []);

  useEffect(() => {
    load();
    handleLoginRequired(() => {
      setNotice(t("admin-api.err_login_required"));
      setState((s) => (s ? { ...s, logged_in: false, csrf: "" } : s));
    });
  }, [load]);

  let body: React.ReactNode;
  if (!state)
    body = error ? (
      <div className="flex h-full flex-col items-center justify-center gap-3 bg-page px-6 text-center">
        <p className="text-[13px] text-ink-2">{error}</p>
        <Button icon="retry" onClick={load}>
          {t("admin-app.retry")}
        </Button>
      </div>
    ) : (
      <div className="flex h-full items-center justify-center bg-page">
        <Spinner size={16} />
      </div>
    );
  else if (!state.password_set) body = <Setup state={state} onDone={setState} />;
  else if (!state.logged_in) body = <Login key={notice} state={state} notice={notice} onDone={(s) => (setNotice(""), setState(s))} />;
  else
    body = (
      <Shell
        hubName={state.hub_name}
        onLogout={() => {
          setCsrf("");
          setNotice("");
          setState({ ...state, logged_in: false, csrf: "" });
        }}
      />
    );
  return (
    <>
      {body}
      <Toasts />
    </>
  );
}
