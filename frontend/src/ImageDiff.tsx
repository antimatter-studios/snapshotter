import { useEffect, useState } from "react";
import type { FileVersions } from "./api";
import { bytes } from "./format";
import { useTranslation } from "react-i18next";
import { comparePixels, type PixelComparison } from "./comparePixels";

/**
 * Two versions of a picture.
 *
 * A line-by-line comparison has nothing to say about a screenshot, which is why
 * this exists separately rather than as a mode of the text view. What someone
 * wants from two versions of an image is to see both, and — when they look alike
 * — a way to tell whether anything moved.
 *
 * Hence two modes. Side by side answers "what are these", and is the default
 * because it is the one that never misleads. Fade puts them in the same place and
 * lets you cross-dissolve, which is how a shifted button or a changed colour
 * becomes obvious; laid side by side those are nearly impossible to spot, since
 * the eye has to carry a memory across the gap.
 */
export function ImageDiff({ versions, leftLabel }: { versions: FileVersions; leftLabel: string }) {
  const { t } = useTranslation();
  type Mode = "side" | "fade" | "difference";
  const [mode, setMode] = useState<Mode>("side");
  // 0 is entirely the left version, 1 entirely the right.
  const [mix, setMix] = useState(0.5);
  const [pixels, setPixels] = useState<PixelComparison | null>(null);
  const [pixelsDiffer, setPixelsDiffer] = useState(false);

  const bothPresent = !!versions.leftImage && !!versions.rightImage;

  // Computed only when asked for. It decodes both pictures and walks every pixel,
  // which is quick for a screenshot and not worth doing for someone who only
  // wanted to look at the two.
  useEffect(() => {
    if (mode !== "difference" || !bothPresent) return;
    let live = true;
    setPixels(null);
    setPixelsDiffer(false);
    comparePixels(versions.leftImage!, versions.rightImage!)
      .then((result) => {
        if (!live) return;
        if (!result) setPixelsDiffer(true);
        else setPixels(result);
      })
      .catch(() => live && setPixelsDiffer(true));
    // Discards an answer that arrived after the file changed underneath it.
    return () => {
      live = false;
    };
  }, [mode, bothPresent, versions.leftImage, versions.rightImage]);

  return (
    <div className="image-diff">
      <div className="image-diff-bar">
        {/* Only offered when there are two pictures to fade between. With one
            side missing the control would be a switch that does nothing. */}
        {bothPresent && (
          <div className="image-modes">
            <button className={mode === "side" ? "on" : ""} onClick={() => setMode("side")}>
              {t("diff.imageSideBySide")}
            </button>
            <button className={mode === "fade" ? "on" : ""} onClick={() => setMode("fade")}>
              {t("diff.imageOverlay")}
            </button>
            <button className={mode === "difference" ? "on" : ""} onClick={() => setMode("difference")}>
              {t("diff.imageDifference")}
            </button>
          </div>
        )}
        {mode === "fade" && bothPresent && (
          <label className="image-fade">
            <span className="visually-hidden">{t("diff.fade")}</span>
            <input
              type="range"
              min={0}
              max={1}
              step={0.01}
              value={mix}
              onChange={(e) => setMix(Number(e.target.value))}
            />
          </label>
        )}
        {versions.identical && <span className="image-identical">{t("diff.imageIdentical")}</span>}
        {mode === "difference" && pixelsDiffer && (
          <span className="image-identical">{t("diff.imageSizeDiffers")}</span>
        )}
        {mode === "difference" && pixels && (
          <span className="image-identical">
            {pixels.percentChanged === 0
              ? t("diff.imageNoPixelChange")
              : t("diff.imageDiffersInPixels", { n: pixels.percentChanged.toFixed(2) })}
          </span>
        )}
      </div>

      {mode === "difference" && bothPresent ? (
        <div className="image-stack">
          {pixels ? <img src={pixels.mask} alt="" className="pixel-mask" /> : null}
        </div>
      ) : mode === "fade" && bothPresent ? (
        // Stacked, with the right version faded over the left. Both are given the
        // same box so a resized picture shows as a picture that does not line up,
        // which is itself the answer.
        <div className="image-stack">
          <img src={versions.leftImage ?? ""} alt="" />
          <img src={versions.rightImage ?? ""} alt="" style={{ opacity: mix }} />
        </div>
      ) : (
        <div className="image-pair">
          <ImageSide
            src={versions.leftImage}
            label={leftLabel}
            dims={versions.leftDims}
            size={versions.leftSize}
            exists={versions.leftExists}
            // Missing on the left means the picture was added after the snapshot
            // was taken, which is a different fact from the one on the right.
            missing={t("diff.imageAdded", { version: leftLabel })}
          />
          <ImageSide
            src={versions.rightImage}
            label={versions.rightLabel}
            dims={versions.rightDims}
            size={versions.rightSize}
            exists={versions.rightExists}
            // Missing on the right means it is gone — the case someone browsing a
            // snapshot is most often here to find.
            missing={t("diff.imageDeleted", { version: versions.rightLabel })}
          />
        </div>
      )}
    </div>
  );
}

function ImageSide({
  src,
  label,
  dims,
  size,
  exists,
  missing,
}: {
  // Optional because the service omits them when empty: a side past the cap has
  // no data URI, and a format Go cannot decode has no dimensions.
  src?: string;
  label: string;
  dims?: string;
  size: number;
  exists: boolean;
  /** What to say when there is no picture on this side. */
  missing: string;
}) {
  const { t } = useTranslation();

  return (
    <figure className="image-side">
      <figcaption>
        <strong>{label}</strong>
        {/* Dimensions answer "was it resized", which the byte size alone cannot:
            a recompressed picture changes size without changing shape. */}
        <span>{exists ? [dims, bytes(size)].filter(Boolean).join(" · ") : "—"}</span>
      </figcaption>
      {src ? (
        <img src={src} alt="" />
      ) : (
        // A side with nothing on it says which of the two reasons applies: the
        // picture is not there, or it is there and too big to show.
        <p className={exists ? "empty-note" : "empty-note image-absent"}>
          {exists ? t("diff.imageTooLarge") : missing}
        </p>
      )}
    </figure>
  );
}
