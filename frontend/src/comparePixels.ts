import pixelmatch from "pixelmatch";

/**
 * Which pixels changed between two pictures, via pixelmatch.
 *
 * The comparison itself is pixelmatch's: it does perceptual colour difference in
 * YIQ space and detects anti-aliased edges, both of which a naive per-channel
 * threshold gets wrong — a re-saved JPEG reports as changed everywhere, and a
 * one-pixel text shift reports as a changed outline rather than moved text.
 *
 * What is left here is the part that is genuinely this application's: getting two
 * data URIs into ImageData, refusing a pair of different shapes, and turning the
 * result into something an <img> can show.
 */

export interface PixelComparison {
  /** A picture of the changes, as a data URI. */
  mask: string;
  /** How much of the picture changed, 0 to 100. */
  percentChanged: number;
  width: number;
  height: number;
}

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
 * Returns null when the two are different shapes: overlaying a 1200-wide picture
 * on an 800-wide one compares unrelated pixels and produces a confident,
 * meaningless answer. Saying so is better than answering it.
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
  const changed = pixelmatch(a.data, b.data, out.data, width, height, {
    // pixelmatch's default is 0.1; this is a shade looser because the pictures
    // being compared are usually screenshots re-encoded by different versions of
    // the same program rather than renders of the same scene.
    threshold: 0.15,
    // The unchanged parts are kept, faint, so the changes have something to be
    // located against. A bare mask says what moved but not where it sits.
    includeAA: false,
    alpha: 0.2,
    diffColor: [0xff, 0x00, 0x88],
  });

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
