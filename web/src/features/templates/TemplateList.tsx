import { Link } from "@tanstack/react-router";
import { useDeleteTemplate, useTemplates, type Template } from "@/api/templates";
import { EmptyState, ErrorBanner, IconButton, Skeleton, Table, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SPACE_TEMPLATE_EDIT_PATH, SPACE_TEMPLATE_NEW_PATH, TEMPLATE_EDIT_PATH, TEMPLATE_NEW_PATH } from "@/config";
import { t } from "@/i18n";
import { useConfirm } from "@/features/shell/ConfirmProvider";

const linkButton =
  "inline-flex h-8 items-center gap-1.5 rounded-control border border-border-strong bg-surface px-3 text-sm font-medium text-ink no-underline hover:bg-surface-raised";

/**
 * The templates of the installation, or of one space, with a way to make,
 * change and delete them for whoever keeps them.
 */
export function TemplateList({ spaceKey, canEdit }: { spaceKey?: string; canEdit: boolean }) {
  const { data, isLoading, error } = useTemplates(spaceKey);
  const remove = useDeleteTemplate();
  const toast = useToast();
  const confirm = useConfirm();
  const scope = spaceKey ? "space" : "organization";
  const mine = (data ?? []).filter((tpl) => tpl.scope === scope);
  const failure = error ?? remove.error;

  async function deleteTemplate(tpl: Template) {
    const things = t.kit.confirm.things;
    if (!(await confirm({ noun: things.named(things.template, tpl.name), body: t.templates.confirmDelete }))) return;
    remove.mutate(tpl.key, { onSuccess: () => toast.success(t.templates.deleted(tpl.name)) });
  }

  return (
    <section aria-labelledby="templates-list" className="space-y-3" data-template-list={scope}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="templates-list" className="text-sm font-semibold text-ink">
          {spaceKey ? t.templates.spaceList : t.templates.siteList}
        </h2>
        {canEdit && (
          <Link
            {...(spaceKey ? { to: SPACE_TEMPLATE_NEW_PATH, params: { spaceKey } } : { to: TEMPLATE_NEW_PATH })}
            className={linkButton}
            data-action="new-template"
          >
            <Icon.Plus />
            {t.templates.newTemplate}
          </Link>
        )}
      </div>
      <p className="text-sm text-ink-muted">{spaceKey ? t.templates.spaceIntro : t.templates.siteIntro}</p>
      {!canEdit && <p className="text-sm text-ink-muted">{spaceKey ? t.templates.spaceNotAdmin : t.templates.siteNotAdmin}</p>}
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
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
                        {...(spaceKey
                          ? { to: SPACE_TEMPLATE_EDIT_PATH, params: { spaceKey, templateKey: tpl.key } }
                          : { to: TEMPLATE_EDIT_PATH, params: { templateKey: tpl.key } })}
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
