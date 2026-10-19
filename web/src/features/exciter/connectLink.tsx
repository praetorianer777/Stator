import type { AnchorHTMLAttributes, MouseEvent } from "react";
import { useRouter } from "@tanstack/react-router";
import { EXCITER_SECTION_ID } from "@/config";
import { connectHref, here } from "@/features/armature/connectLink";

type LinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href">;

/** A hint to connect one's Exciter account: opens the token form and comes back here once Exciter accepts the token. */
export function ConnectExciterLink({ onClick, children, ...rest }: LinkProps) {
  const router = useRouter({ warn: false }) as Parameters<typeof here>[0];
  function follow(event: MouseEvent<HTMLAnchorElement>) {
    onClick?.(event);
    // The page may have moved on since the hint was drawn, so the way back is
    // taken at the moment the hint is followed.
    const href = connectHref(here(router), EXCITER_SECTION_ID);
    event.currentTarget.href = href;
    if (event.defaultPrevented || !router || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    router.history.push(href);
  }
  return (
    <a data-action="connect-exciter-hint" {...rest} href={connectHref(here(router), EXCITER_SECTION_ID)} onClick={follow}>
      {children}
    </a>
  );
}
