import "i18next";
import type en from "../locales/en.json";

/**
 * Makes t() reject a key that does not exist.
 *
 * Without this i18next accepts any string and returns the key when it misses,
 * which is a runtime discovery of something the compiler can see. This is the
 * one property the hand-written catalogue had that i18next does not give by
 * default, so it is kept rather than lost in the move.
 */
declare module "i18next" {
  interface CustomTypeOptions {
    resources: { translation: typeof en };
    keySeparator: false;
    nsSeparator: false;
  }
}
