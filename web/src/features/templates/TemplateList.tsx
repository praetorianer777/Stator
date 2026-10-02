import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useDeleteTemplate, useTemplates, type Template } from "@/api/templates";
import { EmptyState, ErrorBanner, IconButton, Skeleton, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TEMPLATE_EDIT_PATH, TEMPLATE_NEW_PATH } from "@/config";
import { t } from "@/i18n";

const linkButton =
  "inline-flex h-8 items-center gap-1.5 rounded-control border border-border-strong bg-surface px-3 text-sm font-medium text-ink no-underline hover:bg-surface-raised";

/**
 * The templates of the organization, or of one space, with a way to make,
 * change and delete them for whoever keeps them.
 */
export function TemplateList({ spaceKey, canEdit }: { spaceKey?: string; canEdit: boolean }) {
  const { data, isLoading, error } = useTemplates(spaceKey);
  const remove = useDeleteTemplate();
  const [notice, setNotice] = useState("");
  const scope = spaceKey ? "space" : "organization";
  const mine = (data ?? []).filter((tpl) => tpl.scope === scope);
  const failure = error ?? remove.error;

  function deleteTemplate(tpl: Template) {
    if (!window.confirm(t.templates.confirmDelete(tpl.name))) return;
    setNotice("");
    remove.mutate(tpl.key, { onSuccess: () => setNotice(t.templates.deleted(tpl.name)) });
  }

  return (
    <section aria-labelledby="templates-list" className="space-y-3" data-template-list={scope}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="templates-list" className="text-sm font-semibold text-ink">
          {spaceKey ? t.templates.spaceList : t.templates.orgList}
        </h2>
        {canEdit && (
          <Link to={TEMPLATE_NEW_PATH} search={spaceKey ? { space: spaceKey } : {}} className={linkButton} data-action="new-template">
            <Icon.Plus />
            {t.templates.newTemplate}
          </Link>
        )}
      </div>
      <p className="text-sm text-ink-muted">{spaceKey ? t.templates.spaceIntro : t.templates.orgIntro}</p>
      {!canEdit && <p className="text-sm text-ink-muted">{spaceKey ? t.templates.spaceNotAdmin : t.templates.orgNotAdmin}</p>}
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-templates-notice>
        {notice}
      </p>
      {isLoading ? (
        <Skeleton />
      ) : mine.length === 0 ? (
        <EmptyState icon={<Icon.Page />} title={t.templates.empty} description={canEdit ? t.templates.emptyBody : undefined} />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>{t.templates.columnName}</Th>
              <Th className="hidden sm:table-cell">{t.templates.columnVariables}</Th>
              <Th className="w-20">
                <span className="sr-only">{t.templates.columnActions}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {mine.map((tpl) => (
              <tr key={tpl.key} data-template-row={tpl.name}>
                <Td className="max-w-0 min-w-0">
                  <p className="truncate font-medium text-ink">{tpl.name}</p>
                  {tpl.description && <p className="truncate text-xs text-ink-muted">{tpl.description}</p>}
                </Td>
                <Td className="hidden text-sm text-ink-muted sm:table-cell">{t.templates.variableCount(tpl.variables.length)}</Td>
                <Td>
                  {tpl.canEdit && (
                    <div className="flex justify-end gap-1">
                      <Link
                        to={TEMPLATE_EDIT_PATH}
                        params={{ templateKey: tpl.key }}
                        aria-label={t.templates.edit(tpl.name)}
                        className="inline-flex size-8 items-center justify-center rounded-control text-ink-muted hover:bg-surface-raised hover:text-ink"
                        data-action="edit-template"
                      >
                        <Icon.Edit />
                      </Link>
                      <IconButton
                        icon={<Icon.Trash />}
                        label={t.templates.delete(tpl.name)}
                        onClick={() => deleteTemplate(tpl)}
                        data-action="delete-template"
                      />
                    </div>
                  )}
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </section>
  );
}
