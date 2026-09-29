import { afterEach, describe, expect, it } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { renderAt } from "@/test/app";

afterEach(cleanup);

describe("the development editor page", () => {
  it("lets the keyboard reach the stored document, which scrolls on its own", async () => {
    await renderAt("/dev/editor");
    const box = await screen.findByRole("region", { name: "Stored document as JSON" });
    expect(box).toHaveAttribute("data-dev-json");
    expect(box).toHaveAttribute("tabindex", "0");
    box.focus();
    expect(document.activeElement).toBe(box);
  });
});
