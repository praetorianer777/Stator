import { useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { brandLogoHref, useBrand, useRemoveBrandLogo, useSetBrandFooter, useSetBrandLogo } from "@/api/brand";
import { Button, ErrorBanner, Field, PageHeader, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BRAND_FOOTER_MAX_LENGTH, BRAND_LOGO_MAX_BYTES } from "@/config";
import { t } from "@/i18n";

/** Where an administrator sets the logo and the footer line the organization's exports carry. */
export function BrandSettings() {
  const { data: brand, isLoading } = useBrand();
  const setLogo = useSetBrandLogo();
  const removeLogo = useRemoveBrandLogo();
  const saveFooter = useSetBrandFooter();
  const fileInput = useRef<HTMLInputElement>(null);
  const [tooBig, setTooBig] = useState(false);
  const [en, setEn] = useState<string>();
  const [de, setDe] = useState<string>();
  const shownEn = en ?? brand?.footer.en ?? "";
  const shownDe = de ?? brand?.footer.de ?? "";
  const fields = saveFooter.error instanceof ApiError ? saveFooter.error.fields : {};
  const href = brand ? brandLogoHref(brand) : null;

  function onFile(file: File | undefined) {
    if (!file) return;
    setTooBig(file.size > BRAND_LOGO_MAX_BYTES);
    if (file.size > BRAND_LOGO_MAX_BYTES) return;
    setLogo.mutate(file);
    if (fileInput.current) fileInput.current.value = "";
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    saveFooter.mutate({ en: shownEn, de: shownDe }, { onSuccess: () => (setEn(undefined), setDe(undefined)) });
  }

  return (
    <div className="mx-auto max-w-3xl" data-brand-settings="">
      <PageHeader crumb={t.settings.title} title={t.brand.title} />
      <p className="mb-4 text-sm text-ink-muted">{t.brand.intro}</p>
      {isLoading && <Skeleton />}
      {brand && (
        <div className="space-y-6">
          <section aria-labelledby="brand-logo" className="space-y-3">
            <h2 id="brand-logo" className="text-sm font-semibold text-ink">
              {t.brand.logo}
            </h2>
            {href ? (
              <img src={href} alt={t.brand.logoAlt} className="max-h-24 max-w-xs rounded-control border border-border bg-surface p-2" data-brand-logo />
            ) : (
              <p className="text-sm text-ink-muted">{t.brand.noLogo}</p>
            )}
            {tooBig && <ErrorBanner>{t.brand.tooBig}</ErrorBanner>}
            {setLogo.error && <ErrorBanner>{setLogo.error.message}</ErrorBanner>}
            {removeLogo.error && <ErrorBanner>{removeLogo.error.message}</ErrorBanner>}
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="secondary"
                icon={<Icon.Upload />}
                loading={setLogo.isPending}
                onClick={() => fileInput.current?.click()}
                data-action="upload-brand-logo"
              >
                {brand.logo ? t.brand.replaceLogo : t.brand.uploadLogo}
              </Button>
              <input
                ref={fileInput}
                type="file"
                accept="image/png,image/jpeg,image/webp"
                className="hidden"
                aria-label={t.brand.chooseLogo}
                data-brand-logo-input
                onChange={(event) => onFile(event.target.files?.[0])}
              />
              {brand.logo && (
                <Button type="button" variant="secondary" loading={removeLogo.isPending} onClick={() => removeLogo.mutate()} data-action="remove-brand-logo">
                  {t.brand.removeLogo}
                </Button>
              )}
            </div>
            <p className="text-sm text-ink-subtle">{t.brand.logoHint}</p>
          </section>
          <form onSubmit={submit} className="space-y-3" aria-label={t.brand.footer}>
            <h2 className="text-sm font-semibold text-ink">{t.brand.footer}</h2>
            {saveFooter.error && !Object.keys(fields).length && <ErrorBanner>{saveFooter.error.message}</ErrorBanner>}
            <Field
              label={t.brand.footerEn}
              value={shownEn}
              maxLength={BRAND_FOOTER_MAX_LENGTH}
              onChange={(event) => setEn(event.target.value)}
              error={fields.en}
              data-brand-footer="en"
            />
            <Field
              label={t.brand.footerDe}
              value={shownDe}
              maxLength={BRAND_FOOTER_MAX_LENGTH}
              onChange={(event) => setDe(event.target.value)}
              error={fields.de}
              data-brand-footer="de"
            />
            <p className="text-sm text-ink-subtle">{t.brand.footerHint}</p>
            <Button type="submit" loading={saveFooter.isPending} data-action="save-brand-footer">
              {t.brand.saveFooter}
            </Button>
          </form>
        </div>
      )}
    </div>
  );
}
