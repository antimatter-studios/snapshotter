import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { FileIcon } from "./FileIcon";

// The glyph beside a file's name. It carries no information a reader needs — the
// name is right there — so the only thing worth pinning is that it always draws
// something, whatever the name turns out to be. A row missing its icon reads as a
// row that failed to load.

function icon(name: string, isDir = false) {
  const { container } = render(<FileIcon name={name} isDir={isDir} />);
  return container.firstElementChild;
}

describe("the icon beside a name", () => {
  it("draws a folder for a folder", () => {
    expect(icon("projects", true)?.tagName.toLowerCase()).toBe("svg");
  });

  it("draws something for an extension it knows", () => {
    expect(icon("notes.md")).not.toBeNull();
  });

  it("draws something for an extension it has never seen", () => {
    // .sparsebundle, .aae, whatever the next application invents. An unknown kind
    // is the common case on a real disk, not the exception.
    expect(icon("archive.sparsebundle")).not.toBeNull();
  });

  it("draws something for a file with no extension at all", () => {
    expect(icon("Makefile")).not.toBeNull();
  });

  // A leading dot names the file rather than its kind: .bundle is not a "bundle"
  // file, and .gitignore is not a file of type "gitignore". Treating it as an
  // extension gave dotfiles the icon of whatever their name happened to spell.
  it("does not read a leading dot as an extension", () => {
    const dotfile = icon(".bundle");
    const real = icon("thing.bundle");

    expect(dotfile).not.toBeNull();
    expect(dotfile?.outerHTML).not.toBe(real?.outerHTML);
  });
});
