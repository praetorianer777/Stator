import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { applyLanguage } from "@/i18n";
import { Through } from "./Through";

afterEach(() => {
  cleanup();
  applyLanguage("");
});

describe("the through tag", () => {
  it("names the token or app something came through, and says which it is to a screen reader", () => {
    render(<Through via={{ kind: "oauth", name: "Desk assistant" }} />);
    const tag = screen.getByText(/through Desk assistant/);
    expect(tag).toHaveAttribute("data-via", "oauth");
    expect(tag).toHaveTextContent("through Desk assistant (a connected app)");
  });

  it("says nothing for what a person wrote themselves", () => {
    const { container } = render(<Through via={null} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("speaks German", () => {
    applyLanguage("de");
    render(<Through via={{ kind: "token", name: "Nachtlauf" }} />);
    expect(screen.getByText(/über Nachtlauf/)).toHaveTextContent("über Nachtlauf (ein persönliches Zugriffstoken)");
  });
});
