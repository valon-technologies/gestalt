import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// control-* utilities ride theme tokens (--control-h/-fz/-size) that stock
// tailwind-merge can't classify, so it won't collapse them against core
// h-*/size-*/text-* utilities — a className override would then lose to the
// variant by CSS source order. Register them in the matching groups so an
// override (text-4xl, h-auto, …) reliably wins.
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      "font-size": [{ text: ["control-xs", "control-sm", "control-default", "control-lg"] }],
      h: [{ h: ["control-xs", "control-sm", "control-default", "control-lg"] }],
      size: [{ size: ["control-xs", "control-sm", "control-default", "control-lg"] }],
    },
  },
});

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
