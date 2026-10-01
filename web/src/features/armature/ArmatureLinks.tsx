import { useArmatureAccount, useArmatureLinks } from "@/api/armature";
import type { Page } from "@/api/pages";
import { SectionTitle, Tag } from "@/components/ui";
import { t } from "@/i18n";
import { issueUrl } from "./issueKeys";

/**
 * Which issues list this page in Armature: one row per issue the published
 * version names, with whether its link got there and, when not, why.
 */
export function ArmatureLinks({ page }: { page: Page }) {
  const links = useArmatureLinks(page.id, page.version, !page.unpublished);
  const account = useArmatureAccount();
  const rows = links.data ?? [];
  if (rows.length === 0) return null;
  const baseUrl = account.data?.baseUrl ?? null;

  return (
    <section aria-labelledby="armature-links-title" className="mt-10" data-armature-links="">
      <SectionTitle id="armature-links-title">{t.armature.links.title}</SectionTitle>
      <p className="mt-1 text-sm text-ink-muted">{t.armature.links.intro}</p>
      <ul className="mt-2 flex flex-col gap-2" aria-labelledby="armature-links-title">
        {rows.map((link) => (
          <li key={link.key} className="text-sm" data-armature-link={link.key} data-state={link.state}>
            <span className="flex flex-wrap items-center gap-2">
              {baseUrl ? (
                <a href={issueUrl(baseUrl, link.key)} className="font-medium text-ink hover:underline">
                  {link.key}
                </a>
              ) : (
                <span className="font-medium text-ink">{link.key}</span>
              )}
              <Tag>{t.armature.links.states[link.state]}</Tag>
            </span>
            {link.state === "failed" && link.error && <p className="mt-0.5 text-ink-muted">{link.error}</p>}
          </li>
        ))}
      </ul>
    </section>
  );
}
