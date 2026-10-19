import { useEffect, useSyncExternalStore, type AnchorHTMLAttributes, type MouseEvent } from "react";
import { useRouter } from "@tanstack/react-router";
import { useArmatureAccount } from "@/api/armature";
import { ARMATURE_CONNECT_PATH, ARMATURE_RETURN_MAX_LENGTH, ARMATURE_RETURN_PARAM, ARMATURE_SECTION_ID, ARMATURE_TOKENS_PATH, PROFILE_PATH } from "@/config";
import { t } from "@/i18n";

// Control characters, a backslash or whitespace in a path are never written by
// this application, but browsers fold some of them away and so turn a path into
// another host.
const SPACE_CODE = 0x20;
const DELETE_CODE = 0x7f;
const BACKSLASH_CODE = 0x5c;

function hasUnsafeCharacter(value: string): boolean {
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code <= SPACE_CODE || code === DELETE_CODE || code === BACKSLASH_CODE) return true;
  }
  return false;
}

/** A path of this application to go back to after connecting, never another site and never the profile page itself. */
export function safeReturn(value: unknown): string | undefined {
  if (typeof value !== "string" || value.length > ARMATURE_RETURN_MAX_LENGTH) return undefined;
  if (!value.startsWith("/") || value.startsWith("//") || hasUnsafeCharacter(value)) return undefined;
  let url: URL;
  try {
    url = new URL(value, window.location.origin);
  } catch {
    return undefined;
  }
  if (url.origin !== window.location.origin || url.pathname === PROFILE_PATH) return undefined;
  return `${url.pathname}${url.search}${url.hash}`;
}

/**
 * A partner's section of the profile, Armature's unless section names another, with the
 * token field focused and the way back to where the person was.
 */
export function connectHref(from: string | undefined, section: string = ARMATURE_SECTION_ID): string {
  const back = safeReturn(from);
  const query = back ? `?${new URLSearchParams({ [ARMATURE_RETURN_PARAM]: back })}` : "";
  return `${PROFILE_PATH}${query}#${section}`;
}

/** The server's start of a one-click connect, which comes back to the profile and from there to where the person was. */
export function appConnectHref(from: string | undefined): string {
  const back = safeReturn(from);
  const query = back ? `?${new URLSearchParams({ [ARMATURE_RETURN_PARAM]: back })}` : "";
  return `${ARMATURE_CONNECT_PATH}${query}`;
}

/** Armature's page for making tokens, under the address the organization connected. */
export function tokensUrl(baseUrl: string): string {
  return `${baseUrl.replace(/\/+$/, "")}${ARMATURE_TOKENS_PATH}`;
}

type Router = ReturnType<typeof useRouter> | undefined;

// Hints are also drawn inside the editor's node views and in tests without a
// router, where the browser's address is the only one there is.
export function here(router: Router): string {
  if (router) return router.state.location.href;
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

type LinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href">;

/**
 * A connect hint: where Armature offers connected apps it goes straight to Armature's consent page,
 * otherwise it opens the token form; either way it comes back here once the account is connected.
 */
export function ConnectArmatureLink({ onClick, children, ...rest }: LinkProps) {
  const router = useRouter({ warn: false }) as Router;
  const { data: account } = useArmatureAccount();
  // An api from before connected apps answers no app.
  const oneClick = Boolean(account?.app?.offered);
  const hrefOf = oneClick ? appConnectHref : connectHref;
  function follow(event: MouseEvent<HTMLAnchorElement>) {
    onClick?.(event);
    // The page may have moved on since the hint was drawn, so the way back is
    // taken at the moment the hint is followed.
    const href = hrefOf(here(router));
    event.currentTarget.href = href;
    // The one-click start is the server's, which the browser has to load itself.
    if (oneClick || event.defaultPrevented || !router || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    router.history.push(href);
  }
  return (
    <a data-action="connect-armature-hint" data-one-click={oneClick ? "" : undefined} {...rest} href={hrefOf(here(router))} onClick={follow}>
      {children}
    </a>
  );
}

// The notice outlives the profile page that raises it and is drawn by the shell
// on the page the person returns to, so it is kept outside any component.
let returnedTo: string | null = null;
let returnedSaying: string | null = null;
const listeners = new Set<() => void>();

function emit() {
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Says, on the page at path, that the account is now connected; saying names which, Armature's when left out. */
export function announceReturn(path: string, saying?: string) {
  returnedTo = new URL(path, window.location.origin).pathname;
  returnedSaying = saying ?? null;
  emit();
}

/** Takes the notice down. */
export function dismissReturn() {
  if (returnedTo === null) return;
  returnedTo = null;
  emit();
}

/** The notice on the page a person came back to after connecting their account; it goes when they move on. */
export function ReturnNotice({ pathname }: { pathname: string }) {
  const path = useSyncExternalStore(subscribe, () => returnedTo);
  // Raised on the profile page before the way back is taken; only a page other
  // than both means the person has moved on.
  useEffect(() => {
    if (path !== null && path !== pathname && pathname !== PROFILE_PATH) dismissReturn();
  }, [path, pathname]);
  if (path === null || path !== pathname) return null;
  return (
    <div
      role="status"
      className="mb-4 flex items-center justify-between gap-3 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink"
      data-armature-returned
    >
      <span>{returnedSaying ?? t.armature.account.returned}</span>
      <button type="button" className="text-ink-muted underline hover:text-ink" onClick={dismissReturn}>
        {t.common.close}
      </button>
    </div>
  );
}
