/**
 * Which pixels changed between two pictures.
 *
 * There is a package for this — pixelmatch — and it is a good one. This is here
 * instead because the whole job is forty lines against a canvas the browser
 * already provides, and a dependency that has to be audited, updated and
 * bundled is a poor trade for that.
 *
 * The comparison is deliberately blunt. Anti-aliasing and JPEG ringing move
 * pixels by a few values without anything having actually changed, so a
 * threshold below which a pixel counts as unchanged is what separates "this
 * button moved" from "this was re-saved at a different quality".
 */

/** Per-channel difference below which two pixels are treated as the same. */
const channelTolerance = 24;

export interface PixelComparison {
  /** A picture of the changes, as a data URI, or empty when there are none. */
  mask: string;
  /** How much of the picture changed, 0 to 100. */
  percentChanged: number;
  width: number;
  height: number;
}

/** Loads a data URI into something with pixels that can be read. */
function load(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error("the picture could not be decoded"));
    img.src = src;
  });
}

function pixels(img: HTMLImageElement, width: number, height: number): ImageData | null {
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  if (!ctx) return null;
  ctx.drawImage(img, 0, 0);
  return ctx.getImageData(0, 0, width, height);
}

/**
 * Compares two pictures and paints what moved.
 *
 * Returns null when they are different shapes: overlaying a 1200-wide picture on
 * an 800-wide one compares unrelated pixels and produces a confident, meaningless
 * answer. Saying so is better than answering it.
 */
export async function comparePixels(leftSrc: string, rightSrc: string): Promise<PixelComparison | null> {
  const [left, right] = await Promise.all([load(leftSrc), load(rightSrc)]);
  if (left.naturalWidth !== right.naturalWidth || left.naturalHeight !== right.naturalHeight) {
    return null;
  }

  const width = left.naturalWidth;
  const height = left.naturalHeight;
  const a = pixels(left, width, height);
  const b = pixels(right, width, height);
  if (!a || !b) return null;

  const out = new ImageData(width, height);
  let changed = 0;

  for (let i = 0; i < a.data.length; i += 4) {
    const dr = Math.abs(a.data[i] - b.data[i]);
    const dg = Math.abs(a.data[i + 1] - b.data[i + 1]);
    const db = Math.abs(a.data[i + 2] - b.data[i + 2]);
    const da = Math.abs(a.data[i + 3] - b.data[i + 3]);
    const moved = dr > channelTolerance || dg > channelTolerance || db > channelTolerance || da > channelTolerance;

    if (moved) {
      changed++;
      // Magenta: it survives being drawn over a screenshot of almost anything,
      // which red does not — red is already in error text, warning badges and
      // half the screenshots anyone would be comparing.
      out.data[i] = 0xff;
      out.data[i + 1] = 0x00;
      out.data[i + 2] = 0x88;
      out.data[i + 3] = 0xff;
    } else {
      // What did not change is kept, dimmed and desaturated, so the changes have
      // something to be located against. A bare mask on black says what moved but
      // not where it sits.
      const grey = (a.data[i] + a.data[i + 1] + a.data[i + 2]) / 3;
      out.data[i] = grey;
      out.data[i + 1] = grey;
      out.data[i + 2] = grey;
      out.data[i + 3] = Math.round(a.data[i + 3] * 0.22);
    }
  }

  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) return null;
  ctx.putImageData(out, 0, 0);

  return {
    mask: canvas.toDataURL("image/png"),
    percentChanged: (changed / (width * height)) * 100,
    width,
    height,
  };
}
