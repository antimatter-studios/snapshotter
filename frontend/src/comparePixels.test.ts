import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { comparePixels } from "./comparePixels";

// jsdom implements no canvas and no ImageData, so this module is unreachable
// without standing in for that part of the browser. The doubles below are the
// browser only: decoding a source into a raster, and holding a raster in a
// canvas. pixelmatch does its real comparison against real bytes, and the rules
// under test are this application's own — refusing a mismatched pair, and what
// share of the picture is reported as changed.

interface Raster {
  width: number;
  height: number;
  data: Uint8ClampedArray;
}

/** Sources the fake decoder knows about, keyed by the string a view would set. */
const pictures = new Map<string, Raster>();

/** A single colour, everywhere. */
function flat(width: number, height: number, rgba: [number, number, number, number]): Raster {
  const data = new Uint8ClampedArray(width * height * 4);
  for (let i = 0; i < data.length; i += 4) data.set(rgba, i);
  return { width, height, data };
}

function withPixel(base: Raster, index: number, rgba: [number, number, number, number]): Raster {
  const data = new Uint8ClampedArray(base.data);
  data.set(rgba, index * 4);
  return { ...base, data };
}

const black: [number, number, number, number] = [0, 0, 0, 255];
const white: [number, number, number, number] = [255, 255, 255, 255];
/** comparePixels marks changes in this colour, so it is how a change is counted. */
const marked: [number, number, number, number] = [0xff, 0x00, 0x88, 255];

/** Whatever the last putImageData was handed, so the mask can be inspected. */
let painted: { data: Uint8ClampedArray } | null = null;
let contextAvailable = true;

class FakeImage {
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  naturalWidth = 0;
  naturalHeight = 0;
  #src = "";

  set src(value: string) {
    this.#src = value;
    const picture = pictures.get(value);
    // Decoding is asynchronous in a browser, and comparePixels awaits both
    // pictures together — resolving synchronously here would not exercise that.
    queueMicrotask(() => {
      if (!picture) {
        this.onerror?.();
        return;
      }
      this.naturalWidth = picture.width;
      this.naturalHeight = picture.height;
      this.onload?.();
    });
  }

  get src() {
    return this.#src;
  }
}

function countPixels(data: Uint8ClampedArray, rgb: [number, number, number]) {
  let found = 0;
  for (let i = 0; i < data.length; i += 4) {
    if (data[i] === rgb[0] && data[i + 1] === rgb[1] && data[i + 2] === rgb[2]) found++;
  }
  return found;
}

beforeEach(() => {
  painted = null;
  contextAvailable = true;
  pictures.clear();

  class FakeImageData {
    data: Uint8ClampedArray;
    constructor(
      public width: number,
      public height: number,
    ) {
      this.data = new Uint8ClampedArray(width * height * 4);
    }
  }

  Object.assign(globalThis, { Image: FakeImage, ImageData: FakeImageData });

  HTMLCanvasElement.prototype.getContext = function (this: HTMLCanvasElement) {
    if (!contextAvailable) return null;
    let drawn: Raster | undefined;
    return {
      drawImage: (img: { src: string }) => {
        drawn = pictures.get(img.src);
      },
      // The canvas is asked for exactly the region it was sized to, so the
      // raster it was handed is the whole answer.
      getImageData: () => drawn,
      putImageData: (out: { data: Uint8ClampedArray }) => {
        painted = out;
      },
    } as unknown as CanvasRenderingContext2D;
  } as typeof HTMLCanvasElement.prototype.getContext;

  HTMLCanvasElement.prototype.toDataURL = () => "data:image/png;base64,stand-in";
});

afterEach(() => {
  pictures.clear();
});

describe("comparing two pictures", () => {
  it("reports nothing changed when they are the same", async () => {
    pictures.set("left", flat(10, 10, black));
    pictures.set("right", flat(10, 10, black));

    const result = await comparePixels("left", "right");

    expect(result).not.toBeNull();
    expect(result?.percentChanged).toBe(0);
    expect(result?.width).toBe(10);
    expect(result?.height).toBe(10);
    expect(result?.mask).toMatch(/^data:image\/png/);
    // The unchanged picture is kept faint underneath, so the mask is not blank —
    // but none of it is marked as a change.
    expect(countPixels(painted!.data, [marked[0], marked[1], marked[2]])).toBe(0);
  });

  it("counts one changed pixel in a hundred as one per cent", async () => {
    const base = flat(10, 10, black);
    pictures.set("left", base);
    pictures.set("right", withPixel(base, 0, white));

    const result = await comparePixels("left", "right");

    expect(result?.percentChanged).toBeCloseTo(1, 5);
    expect(countPixels(painted!.data, [marked[0], marked[1], marked[2]])).toBe(1);
  });

  it("refuses a pair of different shapes rather than answering", async () => {
    // Overlaying a wider picture on a narrower one compares unrelated pixels and
    // produces a confident, meaningless number. Saying nothing is the answer.
    pictures.set("left", flat(12, 10, black));
    pictures.set("right", flat(10, 10, black));

    expect(await comparePixels("left", "right")).toBeNull();
  });

  it("refuses when differing only in height", async () => {
    pictures.set("left", flat(10, 10, black));
    pictures.set("right", flat(10, 14, black));

    expect(await comparePixels("left", "right")).toBeNull();
  });

  it("says the picture could not be decoded rather than failing quietly", async () => {
    pictures.set("left", flat(10, 10, black));

    await expect(comparePixels("left", "not-a-picture")).rejects.toThrow(/could not be decoded/);
  });

  it("gives up rather than throwing when the canvas yields no context", async () => {
    pictures.set("left", flat(10, 10, black));
    pictures.set("right", flat(10, 10, black));
    contextAvailable = false;

    expect(await comparePixels("left", "right")).toBeNull();
  });
});
