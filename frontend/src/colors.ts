// Sunburst palette.
//
// Hue is inherited from the top-level child an arc descends from, so a whole
// branch reads as one colour family and the eye can follow it outward.
// Lightness falls with depth; packages get a distinct hue so a .app never
// looks like an ordinary folder.

import { KIND_PACKAGE, FLAG_NOPERM, FLAG_EXCLUDED, FLAG_MOUNTPT, FLAG_CLOUD } from './api';

export interface Palette {
  name: string;
  label: string;
  hues: number[];
}

export const PALETTES: Palette[] = [
  { name: 'spectrum', label: 'Spectrum', hues: [205, 150, 42, 12, 330, 265, 180, 95, 20, 300] },
  { name: 'warm', label: 'Warm', hues: [18, 36, 4, 50, 350, 28, 62, 12, 40, 0] },
  { name: 'cool', label: 'Cool', hues: [205, 225, 180, 260, 195, 240, 165, 285, 210, 150] },
  { name: 'mono', label: 'Graphite', hues: [210, 210, 210, 210, 210, 210, 210, 210, 210, 210] },
];

const PACKAGE_HUE = 278;

export function paletteByName(name: string): Palette {
  return PALETTES.find((p) => p.name === name) ?? PALETTES[0];
}

export interface ArcStyle {
  fill: string;
  stroke: string;
}

/**
 * arcColor picks the fill for one arc.
 *
 * @param hueIndex which top-level branch this arc belongs to
 * @param depth    rings from the focus (0 = the centre disc)
 * @param hovered  whether the pointer is on this arc or an ancestor of it
 */
export function arcColor(
  palette: Palette,
  hueIndex: number,
  depth: number,
  kind: number,
  flags: number,
  hovered: boolean,
  dark: boolean,
): ArcStyle {
  if (flags & FLAG_NOPERM) {
    // Inaccessible trees are drawn flat grey and hatched by the renderer;
    // colouring them like data would imply we know what is in there.
    return { fill: dark ? '#4a4a52' : '#c9c9d1', stroke: dark ? '#2a2a30' : '#e8e8ee' };
  }
  const isGroup = hueIndex < 0;
  if (isGroup) {
    // The "N smaller items" wedge has to read as a deliberate group. Too close
    // to the background and it looks like a hole in the data, which is the one
    // thing a disk chart must never suggest.
    const light = (dark ? 34 : 78) + Math.min(depth, 6) * (dark ? 4 : 3) + (hovered ? 10 : 0);
    return {
      fill: `hsl(232 6% ${light}%)`,
      stroke: `hsl(232 6% ${dark ? light + 12 : light - 10}%)`,
    };
  }

  let hue = kind === KIND_PACKAGE ? PACKAGE_HUE : palette.hues[hueIndex % palette.hues.length];
  let sat = kind === KIND_PACKAGE ? 42 : 58;
  if (flags & (FLAG_EXCLUDED | FLAG_MOUNTPT)) sat = 12;
  // Cloud folders hold no local bytes, so they are drawn as a faint outline
  // rather than competing with real usage for attention.
  if (flags & FLAG_CLOUD) {
    hue = 200;
    sat = 16;
  }

  // Rings fade outward so the centre stays dominant and depth is legible
  // without labels.
  const fade = Math.min(depth, 6) * (dark ? 5.5 : 6.5);
  let light = (dark ? 46 : 62) + fade;
  if (hovered) {
    light += dark ? 14 : 10;
    sat += 10;
  }
  const strokeLight = dark ? Math.max(light - 26, 8) : Math.min(light + 22, 98);
  return {
    fill: `hsl(${hue} ${sat}% ${light}%)`,
    stroke: `hsl(${hue} ${Math.max(sat - 20, 5)}% ${strokeLight}%)`,
  };
}

export function centreColor(dark: boolean): string {
  return dark ? '#16171d' : '#ffffff';
}

// resolveTheme turns the "system" preference into a concrete mode.
export function resolveTheme(pref: string): 'light' | 'dark' {
  if (pref === 'light' || pref === 'dark') return pref;
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}
