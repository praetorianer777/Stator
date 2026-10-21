import type { FieldsetHTMLAttributes } from "react";
import type { Space } from "@/api/spaces";
import { Chip } from "@/components/ui";

type SpacePickerProps = Omit<FieldsetHTMLAttributes<HTMLFieldSetElement>, "onChange"> & {
  /** The spaces that may be picked: those the person can see. */
  spaces: Space[];
  picked: string[];
  onChange: (keys: string[]) => void;
  legend: string;
  hint: string;
  hintId: string;
};

/** Spaces to limit a token or a connected app to, by key; which none picked means is the caller's to say. */
export function SpacePicker({ spaces, picked, onChange, legend, hint, hintId, ...rest }: SpacePickerProps) {
  const toggle = (key: string) => onChange(picked.includes(key) ? picked.filter((k) => k !== key) : [...picked, key]);
  return (
    <fieldset aria-describedby={hintId} {...rest}>
      <legend className="text-sm font-medium text-ink">{legend}</legend>
      <p id={hintId} className="mb-2 text-xs text-ink-muted">
        {hint}
      </p>
      <div className="flex flex-wrap gap-1.5">
        {spaces.map((space) => (
          <Chip key={space.key} pressed={picked.includes(space.key)} data-space={space.key} onClick={() => toggle(space.key)}>
            {space.name}
            <span className="text-xs">{space.key}</span>
          </Chip>
        ))}
      </div>
    </fieldset>
  );
}
