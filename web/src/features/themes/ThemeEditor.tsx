import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import {
  useChooseTheme,
  useCreateTheme,
  useDeleteThemeAsset,
  useThemeExamples,
  useUpdateTheme,
  useUploadThemeAsset,
  type Theme,
  type ThemeAsset,
  type ThemeExample,
  type ThemeSpec,
} from "@/api/themes";
import { Button, Card, ColorField, ErrorBanner, Field, IconButton, OptionCard, SectionTitle, Segmented, SelectInput, Switch, Tabs, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import {
  KILOBYTE,
  THEME_ASSET_MAX_BYTES,
  THEME_CSS_MAX_BYTES,
  THEME_CSS_ROWS,
  THEME_CURSOR_PX,
  THEME_ICON_PREVIEW_PX,
  THEME_MAX_HOTSPOT,
  THEME_MAX_RADIUS,
  THEME_PREVIEW_DEBOUNCE_MS,
} from "@/config";
import { t } from "@/i18n";
import { compileTheme, emptySpec } from "@/lib/theme-css";
import { CURSOR_KINDS, SHADOW_KEYS, TOKEN_GROUPS, TOKEN_NAMES, tokenLabel } from "@/lib/theme-tokens";
import { setThemePreview } from "./ThemeLoader";

type TabID = "colours" | "type" | "shape" | "cursors" | "icons" | "backdrop" | "files" | "advanced";
type Mode = "light" | "dark";
type Effect = "none" | "constellation" | "confetti";

const tabs = (): Array<{ value: TabID; label: string }> => [
  { value: "colours", label: t.themes.editor.tabColours },
  { value: "type", label: t.themes.editor.tabType },
  { value: "shape", label: t.themes.editor.tabShape },
  { value: "cursors", label: t.themes.editor.tabCursors },
  { value: "icons", label: t.themes.editor.tabIcons },
  { value: "backdrop", label: t.themes.editor.tabBackdrop },
  { value: "files", label: t.themes.editor.tabFiles },
  { value: "advanced", label: t.themes.editor.tabAdvanced },
];

const UPLOAD_ACCEPT = "image/png,image/jpeg,image/webp,image/gif,image/svg+xml,.svg,.woff,.woff2,font/woff,font/woff2";

/** The glyphs the kit draws, by the name a theme keys them on. */
const glyphs: Array<{ name: string; Glyph: (typeof Icon)[keyof typeof Icon] }> = Object.values(Icon)
  .map((Glyph) => ({ name: Glyph.glyph, Glyph }))
  .sort((a, b) => a.name.localeCompare(b.name));

function isFont(a: ThemeAsset): boolean {
  return a.contentType.startsWith("font/");
}
function isImage(a: ThemeAsset): boolean {
  return a.contentType.startsWith("image/");
}

/**
 * One theme, part by part. Nothing is written until Save; Preview shows the
 * draft on this very page, and leaving the page shows the chosen theme again.
 */
export function ThemeEditor({ theme }: { theme?: Theme }) {
  const navigate = useNavigate();
  const create = useCreateTheme();
  const update = useUpdateTheme();
  const choose = useChooseTheme();
  const [name, setName] = useState(theme?.name ?? "");
  const [shared, setShared] = useState(theme?.shared ?? false);
  const [spec, setSpec] = useState<ThemeSpec>(() => (theme ? structuredClone(theme.spec) : emptySpec()));
  const [tab, setTab] = useState<TabID>("colours");
  const [preview, setPreview] = useState(false);
  const [startedFrom, setStartedFrom] = useState("blank");
  const [notice, setNotice] = useState("");
  const assets = useMemo(() => theme?.assets ?? [], [theme?.assets]);
  const saving = create.isPending || update.isPending;
  const patch = (change: (draft: ThemeSpec) => void) =>
    setSpec((current) => {
      const draft = structuredClone(current);
      change(draft);
      return draft;
    });

  // The draft is compiled after it settles, so typing a colour does not
  // restyle the page on every keystroke.
  useEffect(() => {
    if (!preview) {
      setThemePreview(null);
      return;
    }
    const timer = window.setTimeout(
      () => setThemePreview(compileTheme({ id: theme?.id ?? "new", spec, assets }), spec.effect ?? null),
      THEME_PREVIEW_DEBOUNCE_MS,
    );
    return () => window.clearTimeout(timer);
  }, [preview, spec, theme?.id, assets]);
  useEffect(() => () => setThemePreview(null), []);

  function save(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    if (theme) {
      update.mutate({ id: theme.id, name: name.trim(), shared, spec }, { onSuccess: (saved) => setNotice(t.themes.saved(saved.theme.name)) });
    } else {
      create.mutate(
        { name: name.trim(), shared, spec },
        { onSuccess: (made) => navigate({ to: "/settings/themes/$themeId", params: { themeId: made.theme.id } }) },
      );
    }
  }

  const failure = create.error ?? update.error ?? choose.error;
  return (
    <form onSubmit={save} className="space-y-4" noValidate data-theme-editor={theme?.id ?? "new"}>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {notice && (
        <p role="status" className="text-sm text-ink-muted" data-theme-notice>
          {notice}
        </p>
      )}
      <Card className="p-4">
        <div className="flex flex-wrap items-end gap-4">
          <Field label={t.themes.editor.name} id="field-theme-name" value={name} onChange={(ev) => setName(ev.target.value)} required className="w-64" />
          <span className="flex items-center gap-2 pb-2 text-sm text-ink-muted">
            <Switch checked={shared} onChange={setShared} label={t.themes.editor.shared} data-theme-shared={shared ? "true" : "false"} />
            {t.themes.editor.shared}
          </span>
          <span className="flex items-center gap-2 pb-2 text-sm text-ink-muted">
            <Switch checked={preview} onChange={setPreview} label={t.themes.editor.preview} data-action="preview-theme" />
            {t.themes.editor.preview}
          </span>
          <span className="ml-auto flex gap-2 pb-1">
            {theme && !theme.active && (
              <Button
                type="button"
                variant="secondary"
                loading={choose.isPending}
                onClick={() => choose.mutate(theme.id, { onSuccess: () => setNotice(t.themes.nowUsing(theme.name)) })}
                data-action="use-theme"
              >
                {t.themes.use}
              </Button>
            )}
            <Button type="submit" loading={saving} disabled={!name.trim()} data-action="save-theme">
              {t.themes.editor.save}
            </Button>
          </span>
        </div>
      </Card>

      {!theme && (
        <StartFrom
          chosen={startedFrom}
          onChoose={(example) => {
            setStartedFrom(example?.key ?? "blank");
            setSpec(example ? structuredClone(example.spec) : emptySpec());
            if (example && !name.trim()) setName(example.name);
          }}
        />
      )}

      <Tabs<TabID>
        label={t.themes.editor.tabs}
        value={tab}
        onChange={setTab}
        tabs={tabs().map((each) => ({ ...each, attrs: { "data-theme-tab": each.value } }))}
      />

      {tab === "colours" && <ColoursTab spec={spec} patch={patch} />}
      {tab === "type" && <TypeTab spec={spec} patch={patch} assets={assets} />}
      {tab === "shape" && <ShapeTab spec={spec} patch={patch} />}
      {tab === "cursors" && <CursorsTab spec={spec} patch={patch} assets={assets} />}
      {tab === "icons" && <IconsTab spec={spec} patch={patch} assets={assets} />}
      {tab === "backdrop" && <BackdropTab spec={spec} patch={patch} assets={assets} />}
      {tab === "files" && <FilesTab theme={theme} spec={spec} onNotice={setNotice} />}
      {tab === "advanced" && <AdvancedTab spec={spec} patch={patch} />}
    </form>
  );
}

type Patch = (change: (draft: ThemeSpec) => void) => void;

// A new theme starts blank or from one the product ships; picking one
// replaces the whole draft, so it comes before anything is typed.
function StartFrom({ chosen, onChoose }: { chosen: string; onChoose: (example: ThemeExample | null) => void }) {
  const { data } = useThemeExamples();
  const examples = data?.examples ?? [];
  if (examples.length === 0) return null;
  return (
    <Card className="p-4" data-theme-start>
      <SectionTitle className="mb-3">{t.themes.editor.startFrom}</SectionTitle>
      <div role="radiogroup" aria-label={t.themes.editor.startFrom} className="grid gap-3 sm:grid-cols-3">
        <OptionCard
          title={t.themes.editor.blank}
          description={t.themes.editor.blankBody}
          checked={chosen === "blank"}
          onSelect={() => onChoose(null)}
          data-theme-example="blank"
        />
        {examples.map((example) => (
          <OptionCard
            key={example.key}
            title={example.name}
            description={example.description}
            checked={chosen === example.key}
            onSelect={() => onChoose(example)}
            data-theme-example={example.key}
          />
        ))}
      </div>
    </Card>
  );
}

function stockValue(token: string): string {
  try {
    return getComputedStyle(document.documentElement).getPropertyValue(`--color-${token}`).trim();
  } catch {
    return "";
  }
}

function ColoursTab({ spec, patch }: { spec: ThemeSpec; patch: Patch }) {
  const [mode, setMode] = useState<Mode>("light");
  const set = spec.colors[mode];
  const overridden = Object.keys(set).length;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Segmented<Mode>
          label={t.themes.editor.palette}
          value={mode}
          onChange={setMode}
          options={[
            { value: "light", label: t.themes.editor.light, attrs: { "data-theme-mode": "light" } },
            { value: "dark", label: t.themes.editor.dark, attrs: { "data-theme-mode": "dark" } },
          ]}
        />
        <span className="text-sm text-ink-muted">
          {overridden === 0 ? t.themes.editor.nothingChanged : t.themes.editor.changed(overridden, TOKEN_NAMES.length)}
        </span>
      </div>
      {TOKEN_GROUPS.map((group) => (
        <Card key={group.id} className="p-4">
          <SectionTitle className="mb-3">{t.themes.groups[group.id]}</SectionTitle>
          <div className="grid gap-3 sm:grid-cols-2">
            {group.tokens.map((token) => {
              const value = set[token] ?? "";
              const label = tokenLabel(token);
              return (
                <ColorField
                  key={token}
                  id={`field-token-${token}`}
                  label={label}
                  value={value}
                  placeholder={stockValue(token) || "#000000"}
                  onChange={(next) =>
                    patch((draft) => {
                      if (next.trim() === "") delete draft.colors[mode][token];
                      else draft.colors[mode][token] = next.trim();
                    })
                  }
                  data-token={token}
                  data-token-mode={mode}
                >
                  {value && (
                    <IconButton
                      icon={<Icon.X />}
                      label={t.themes.editor.resetToken(label)}
                      size="sm"
                      onClick={() =>
                        patch((draft) => {
                          delete draft.colors[mode][token];
                        })
                      }
                      data-action="reset-token"
                    />
                  )}
                </ColorField>
              );
            })}
          </div>
        </Card>
      ))}
    </div>
  );
}

function AssetSelect({
  label,
  assets,
  value,
  onChange,
}: {
  label: string;
  assets: ThemeAsset[];
  value: string | undefined;
  onChange: (id: string | undefined) => void;
}) {
  return (
    <SelectInput aria-label={label} controlSize="sm" value={value ?? ""} onChange={(ev) => onChange(ev.target.value || undefined)} className="w-56">
      <option value="">{assets.length === 0 ? t.themes.editor.noFiles : t.themes.editor.chooseFile}</option>
      {assets.map((a) => (
        <option key={a.id} value={a.id}>
          {a.name}
        </option>
      ))}
    </SelectInput>
  );
}

function TypeTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const fonts = assets.filter(isFont);
  return (
    <Card className="space-y-5 p-4">
      {(["sans", "mono"] as const).map((role) => {
        const font = spec.fonts[role];
        const family = role === "sans" ? t.themes.editor.textFamily : t.themes.editor.codeFamily;
        return (
          <div key={role} className="space-y-2" data-theme-font={role}>
            <SectionTitle>{role === "sans" ? t.themes.editor.textFace : t.themes.editor.codeFace}</SectionTitle>
            <div className="flex flex-wrap items-end gap-3">
              <Field
                label={family}
                value={font?.family ?? ""}
                placeholder={role === "sans" ? "Inter" : "JetBrains Mono"}
                onChange={(ev) =>
                  patch((draft) => {
                    const next = ev.target.value;
                    if (next.trim() === "") delete draft.fonts[role];
                    else draft.fonts[role] = { ...(draft.fonts[role] ?? {}), family: next };
                  })
                }
                className="w-64"
                hint={t.themes.editor.familyHint}
              />
              <span className="space-y-1 pb-5">
                <span className="block text-sm font-medium text-ink-muted">{t.themes.editor.file}</span>
                <AssetSelect
                  label={t.themes.editor.fontFile(family)}
                  assets={fonts}
                  value={font?.assetId}
                  onChange={(id) =>
                    patch((draft) => {
                      const current = draft.fonts[role];
                      if (current) draft.fonts[role] = { family: current.family, ...(id ? { assetId: id } : {}) };
                    })
                  }
                />
              </span>
              {font && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="mb-5"
                  onClick={() =>
                    patch((draft) => {
                      delete draft.fonts[role];
                    })
                  }
                >
                  {t.themes.editor.reset}
                </Button>
              )}
            </div>
          </div>
        );
      })}
    </Card>
  );
}

function NumberField({
  label,
  value,
  onChange,
  max,
  placeholder,
}: {
  label: string;
  value: number | undefined;
  onChange: (value: number | undefined) => void;
  max: number;
  placeholder: string;
}) {
  return (
    <Field
      label={label}
      type="number"
      min={0}
      max={max}
      value={value ?? ""}
      placeholder={placeholder}
      onChange={(ev) => onChange(ev.target.value === "" ? undefined : Math.max(0, Math.min(max, Number(ev.target.value))))}
      className="w-28"
    />
  );
}

function ShapeTab({ spec, patch }: { spec: ThemeSpec; patch: Patch }) {
  return (
    <div className="space-y-4">
      <Card className="p-4">
        <SectionTitle className="mb-3">{t.themes.editor.corners}</SectionTitle>
        <div className="flex flex-wrap gap-4">
          <NumberField
            label={t.themes.editor.radiusControl}
            value={spec.shape.radiusControl}
            max={THEME_MAX_RADIUS}
            placeholder="6"
            onChange={(v) =>
              patch((d) => {
                if (v === undefined) delete d.shape.radiusControl;
                else d.shape.radiusControl = v;
              })
            }
          />
          <NumberField
            label={t.themes.editor.radiusOverlay}
            value={spec.shape.radiusOverlay}
            max={THEME_MAX_RADIUS}
            placeholder="8"
            onChange={(v) =>
              patch((d) => {
                if (v === undefined) delete d.shape.radiusOverlay;
                else d.shape.radiusOverlay = v;
              })
            }
          />
        </div>
      </Card>
      <Card className="p-4">
        <SectionTitle className="mb-3">{t.themes.editor.shadows}</SectionTitle>
        <div className="space-y-3">
          {SHADOW_KEYS.map((key) => (
            <Field
              key={key}
              label={t.themes.editor.shadowLabels[key] ?? key}
              value={spec.shadows[key] ?? ""}
              placeholder="0 1px 2px rgb(0 0 0 / 0.1)"
              onChange={(ev) =>
                patch((d) => {
                  if (ev.target.value.trim() === "") delete d.shadows[key];
                  else d.shadows[key] = ev.target.value;
                })
              }
              className="font-mono"
              data-shadow={key}
            />
          ))}
        </div>
      </Card>
    </div>
  );
}

function CursorsTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const images = assets.filter(isImage);
  return (
    <Card className="p-4">
      <p className="mb-4 text-sm text-ink-muted">{t.themes.editor.cursorsIntro(THEME_CURSOR_PX)}</p>
      <div className="space-y-3">
        {CURSOR_KINDS.map((each) => {
          const cursor = spec.cursors[each.kind];
          const label = t.themes.cursors[each.kind] ?? each.kind;
          return (
            <div key={each.kind} className="flex flex-wrap items-end gap-3" data-theme-cursor={each.kind}>
              <span className="w-28 pb-2 text-sm text-ink">{label}</span>
              <span className="space-y-1">
                <span className="block text-sm font-medium text-ink-muted">{t.themes.editor.picture}</span>
                <AssetSelect
                  label={t.themes.editor.cursorPicture(label)}
                  assets={images}
                  value={cursor?.assetId}
                  onChange={(id) =>
                    patch((d) => {
                      if (!id) delete d.cursors[each.kind];
                      else d.cursors[each.kind] = { assetId: id, hotspotX: cursor?.hotspotX ?? 0, hotspotY: cursor?.hotspotY ?? 0 };
                    })
                  }
                />
              </span>
              {cursor && (
                <>
                  <NumberField
                    label={t.themes.editor.pointX}
                    value={cursor.hotspotX}
                    max={THEME_MAX_HOTSPOT}
                    placeholder="0"
                    onChange={(v) =>
                      patch((d) => {
                        d.cursors[each.kind]!.hotspotX = v ?? 0;
                      })
                    }
                  />
                  <NumberField
                    label={t.themes.editor.pointY}
                    value={cursor.hotspotY}
                    max={THEME_MAX_HOTSPOT}
                    placeholder="0"
                    onChange={(v) =>
                      patch((d) => {
                        d.cursors[each.kind]!.hotspotY = v ?? 0;
                      })
                    }
                  />
                </>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
}

function IconsTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const images = assets.filter(isImage);
  const [query, setQuery] = useState("");
  const shown = useMemo(() => glyphs.filter((g) => g.name.includes(query.trim().toLowerCase())), [query]);
  return (
    <Card className="p-4">
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <Field label={t.themes.editor.findIcon} value={query} onChange={(ev) => setQuery(ev.target.value)} className="w-56" />
        <p className="pb-2 text-sm text-ink-muted">{t.themes.editor.iconsIntro}</p>
      </div>
      <div className="divide-y divide-border">
        {shown.map(({ name, Glyph }) => {
          const icon = spec.icons[name];
          return (
            <div key={name} className="flex flex-wrap items-start gap-3 py-2" data-theme-icon={name}>
              <span className="flex w-40 items-center gap-2 pt-2 text-sm text-ink">
                <Glyph size={THEME_ICON_PREVIEW_PX} />
                {name}
              </span>
              <span className="space-y-1">
                <span className="block text-sm font-medium text-ink-muted">{t.themes.editor.picture}</span>
                <AssetSelect
                  label={t.themes.editor.iconPicture(name)}
                  assets={images}
                  value={icon?.assetId}
                  onChange={(id) =>
                    patch((d) => {
                      if (!id && !icon?.paths?.length) delete d.icons[name];
                      else d.icons[name] = { ...(id ? { assetId: id } : {}), ...(icon?.paths?.length ? { paths: icon.paths } : {}) };
                    })
                  }
                />
              </span>
              <Field
                label={t.themes.editor.iconPaths(name)}
                id={`field-icon-${name}`}
                rows={2}
                value={icon?.paths?.join("\n") ?? ""}
                placeholder="M2 8h12"
                onChange={(ev) =>
                  patch((d) => {
                    const paths = ev.target.value
                      .split("\n")
                      .map((line) => line.trim())
                      .filter(Boolean);
                    if (paths.length === 0 && !icon?.assetId) delete d.icons[name];
                    else d.icons[name] = { ...(icon?.assetId ? { assetId: icon.assetId } : {}), ...(paths.length ? { paths } : {}) };
                  })
                }
                className="w-72 font-mono"
              />
              {icon && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="mt-6"
                  onClick={() =>
                    patch((d) => {
                      delete d.icons[name];
                    })
                  }
                >
                  {t.themes.editor.reset}
                </Button>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
}

function BackdropTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const images = assets.filter(isImage);
  const backdrop = spec.backdrop ?? null;
  const effect: Effect = spec.effect === "constellation" || spec.effect === "confetti" ? spec.effect : "none";
  return (
    <Card className="p-4">
      <p className="mb-4 text-sm text-ink-muted">{t.themes.editor.backdropIntro}</p>
      <div className="flex flex-wrap items-end gap-4">
        <span className="space-y-1">
          <span className="block text-sm font-medium text-ink-muted">{t.themes.editor.picture}</span>
          <AssetSelect
            label={t.themes.editor.backdropPicture}
            assets={images}
            value={backdrop?.assetId}
            onChange={(id) =>
              patch((d) => {
                d.backdrop = id ? { assetId: id, fit: backdrop?.fit ?? "cover" } : null;
              })
            }
          />
        </span>
        {backdrop && (
          <span className="pb-0.5">
            <Segmented<"cover" | "tile">
              label={t.themes.editor.fit}
              size="sm"
              value={backdrop.fit}
              onChange={(fit) =>
                patch((d) => {
                  if (d.backdrop) d.backdrop.fit = fit;
                })
              }
              options={[
                { value: "cover", label: t.themes.editor.fitCover },
                { value: "tile", label: t.themes.editor.fitTile },
              ]}
            />
          </span>
        )}
      </div>
      <p className="mt-6 mb-2 text-sm text-ink-muted">{t.themes.editor.effectIntro}</p>
      <Segmented<Effect>
        label={t.themes.editor.effect}
        size="sm"
        value={effect}
        onChange={(next) =>
          patch((d) => {
            if (next === "none") delete d.effect;
            else d.effect = next;
          })
        }
        options={[
          { value: "none", label: t.themes.editor.effectNone, attrs: { "data-theme-effect": "none" } },
          { value: "constellation", label: t.themes.editor.effectConstellation, attrs: { "data-theme-effect": "constellation" } },
          { value: "confetti", label: t.themes.editor.effectConfetti, attrs: { "data-theme-effect": "confetti" } },
        ]}
      />
    </Card>
  );
}

function FilesTab({ theme, spec, onNotice }: { theme?: Theme; spec: ThemeSpec; onNotice: (notice: string) => void }) {
  const uploadAsset = useUploadThemeAsset();
  const deleteAsset = useDeleteThemeAsset();
  const fileInput = useRef<HTMLInputElement>(null);
  const [tooBig, setTooBig] = useState(false);
  if (!theme) {
    return (
      <Card className="p-4">
        <p className="text-sm text-ink-muted">{t.themes.editor.saveFirst}</p>
      </Card>
    );
  }
  const used = new Set<string>([
    ...[spec.fonts.sans?.assetId, spec.fonts.mono?.assetId, spec.backdrop?.assetId].filter((id): id is string => Boolean(id)),
    ...Object.values(spec.cursors).map((c) => c.assetId),
    ...Object.values(spec.icons)
      .map((i) => i.assetId)
      .filter((id): id is string => Boolean(id)),
  ]);
  function onFile(file: File | undefined) {
    if (!file) return;
    if (file.size > THEME_ASSET_MAX_BYTES) {
      setTooBig(true);
      return;
    }
    setTooBig(false);
    uploadAsset.mutate({ id: theme!.id, file }, { onSuccess: (made) => onNotice(t.themes.editor.added(made.asset.name)) });
  }
  return (
    <Card className="space-y-3 p-4">
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          variant="secondary"
          icon={<Icon.Upload />}
          loading={uploadAsset.isPending}
          onClick={() => fileInput.current?.click()}
          data-action="add-theme-file"
        >
          {t.themes.editor.addFile}
        </Button>
        <input
          ref={fileInput}
          type="file"
          accept={UPLOAD_ACCEPT}
          className="hidden"
          aria-label={t.themes.editor.chooseThemeFile}
          data-theme-file-input
          onChange={(ev) => onFile(ev.target.files?.[0])}
        />
        <p className="text-sm text-ink-subtle">{t.themes.editor.filesHint}</p>
      </div>
      {tooBig && <ErrorBanner>{t.themes.editor.tooBig}</ErrorBanner>}
      {uploadAsset.error && <ErrorBanner>{uploadAsset.error.message}</ErrorBanner>}
      {deleteAsset.error && <ErrorBanner>{deleteAsset.error.message}</ErrorBanner>}
      {theme.assets.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.themes.editor.noFilesYet}</p>
      ) : (
        <ul className="divide-y divide-border">
          {theme.assets.map((a) => (
            <li key={a.id} className="flex items-center gap-3 py-2 text-sm" data-theme-asset={a.name}>
              <span className="min-w-0 flex-1 truncate text-ink">{a.name}</span>
              <Tag>{a.contentType}</Tag>
              <span className="text-ink-subtle tabular-nums">{t.themes.editor.kilobytes(Math.max(1, Math.round(a.size / KILOBYTE)))}</span>
              {used.has(a.id) && <Tag>{t.themes.editor.inUse}</Tag>}
              <IconButton
                icon={<Icon.Trash />}
                label={t.themes.editor.removeFile(a.name)}
                size="sm"
                disabled={used.has(a.id) || deleteAsset.isPending}
                onClick={() => {
                  if (window.confirm(t.themes.editor.confirmRemoveFile(a.name))) {
                    deleteAsset.mutate({ id: theme.id, assetId: a.id }, { onSuccess: () => onNotice(t.themes.editor.removed(a.name)) });
                  }
                }}
                data-action="remove-theme-file"
              />
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function AdvancedTab({ spec, patch }: { spec: ThemeSpec; patch: Patch }) {
  const size = new TextEncoder().encode(spec.css).length;
  return (
    <Card className="p-4">
      <Field
        label={t.themes.editor.extraCSS}
        rows={THEME_CSS_ROWS}
        value={spec.css}
        onChange={(ev) =>
          patch((d) => {
            d.css = ev.target.value;
          })
        }
        className="font-mono"
        hint={t.themes.editor.cssHint(Math.round(size / KILOBYTE), THEME_CSS_MAX_BYTES / KILOBYTE)}
        data-theme-css=""
      />
    </Card>
  );
}
