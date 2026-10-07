---
name: Modern Scholarly Wayfinding
colors:
  surface: '#fbf8ff'
  surface-dim: '#dad9e3'
  surface-bright: '#fbf8ff'
  surface-container-lowest: '#ffffff'
  surface-container-low: '#f4f2fd'
  surface-container: '#eeedf7'
  surface-container-high: '#e8e7f1'
  surface-container-highest: '#e3e1ec'
  on-surface: '#1a1b22'
  on-surface-variant: '#434655'
  inverse-surface: '#2f3038'
  inverse-on-surface: '#f1effa'
  outline: '#737686'
  outline-variant: '#c3c6d7'
  surface-tint: '#0053db'
  primary: '#004ac6'
  on-primary: '#ffffff'
  primary-container: '#2563eb'
  on-primary-container: '#eeefff'
  inverse-primary: '#b4c5ff'
  secondary: '#855300'
  on-secondary: '#ffffff'
  secondary-container: '#fea619'
  on-secondary-container: '#684000'
  tertiary: '#006329'
  on-tertiary: '#ffffff'
  tertiary-container: '#007f36'
  on-tertiary-container: '#c7ffca'
  error: '#ba1a1a'
  on-error: '#ffffff'
  error-container: '#ffdad6'
  on-error-container: '#93000a'
  primary-fixed: '#dbe1ff'
  primary-fixed-dim: '#b4c5ff'
  on-primary-fixed: '#00174b'
  on-primary-fixed-variant: '#003ea8'
  secondary-fixed: '#ffddb8'
  secondary-fixed-dim: '#ffb95f'
  on-secondary-fixed: '#2a1700'
  on-secondary-fixed-variant: '#653e00'
  tertiary-fixed: '#7ffc97'
  tertiary-fixed-dim: '#62df7d'
  on-tertiary-fixed: '#002109'
  on-tertiary-fixed-variant: '#005320'
  background: '#fbf8ff'
  on-background: '#1a1b22'
  surface-variant: '#e3e1ec'
typography:
  headline-xl:
    fontFamily: Inter
    fontSize: 36px
    fontWeight: '600'
    lineHeight: 44px
    letterSpacing: -0.02em
  headline-xl-mobile:
    fontFamily: Inter
    fontSize: 28px
    fontWeight: '600'
    lineHeight: 36px
    letterSpacing: -0.015em
  headline-lg:
    fontFamily: Inter
    fontSize: 28px
    fontWeight: '600'
    lineHeight: 36px
    letterSpacing: -0.015em
  headline-md:
    fontFamily: Inter
    fontSize: 22px
    fontWeight: '600'
    lineHeight: 28px
    letterSpacing: -0.01em
  headline-sm:
    fontFamily: Inter
    fontSize: 18px
    fontWeight: '600'
    lineHeight: 24px
    letterSpacing: -0.005em
  body-lg:
    fontFamily: Inter
    fontSize: 16px
    fontWeight: '400'
    lineHeight: 24px
  body-md:
    fontFamily: Inter
    fontSize: 14px
    fontWeight: '400'
    lineHeight: 20px
  body-sm:
    fontFamily: Inter
    fontSize: 13px
    fontWeight: '400'
    lineHeight: 18px
  label-lg:
    fontFamily: Inter
    fontSize: 14px
    fontWeight: '500'
    lineHeight: 20px
  label-md:
    fontFamily: Inter
    fontSize: 12px
    fontWeight: '500'
    lineHeight: 16px
  code-callout:
    fontFamily: JetBrains Mono
    fontSize: 12px
    fontWeight: '500'
    lineHeight: 16px
    letterSpacing: 0.02em
  code-sm:
    fontFamily: JetBrains Mono
    fontSize: 11px
    fontWeight: '400'
    lineHeight: 14px
    letterSpacing: 0.03em
rounded:
  sm: 0.125rem
  DEFAULT: 0.25rem
  md: 0.375rem
  lg: 0.5rem
  xl: 0.75rem
  full: 9999px
spacing:
  gutter: 1rem
  gutter-md: 1.5rem
  gutter-lg: 2rem
  margin: 1rem
  margin-md: 2rem
  margin-lg: 3rem
  space-xs: 0.25rem
  space-sm: 0.5rem
  space-md: 1rem
  space-lg: 1.5rem
  space-xl: 2rem
---

## Brand & Style

This design system embodies a calm, scholarly minimalism built for high-density cataloging, inventory precision, and intellectual wayfinding. Drawing inspiration from modern archival architecture and contemporary editorial design, the interface prioritizes navigational clarity, spatial calm, and purposeful typographic hierarchy over decorative ornamentation. 

The target audience encompasses research librarians, university archivists, avid readers, and operational library staff who interact with extensive inventories over sustained periods. The emotional resonance is one of quiet confidence, institutional trust, and frictionless discovery. Interaction models embrace restrained feedback, crisp tabular structure, and low-cognitive-load layouts that let book covers, classification metadata, and availability statuses serve as primary visual anchors.

## Colors

The palette establishes an unobtrusive foundation anchored by neutral zinc tones, ensuring data legibility and cover art preservation across diverse display conditions.

- **Primary Canvas & Neutral Scale**: Built on Tailwind Zinc (`#FAFAFA` base backgrounds, `#F4F4F5` secondary surfaces, `#E4E4E7` borders, `#71717A` muted metadata, and `#09090B` primary text).
- **Primary Brand Accent (`#2563EB`)**: Used exclusively for focus states, primary navigational signposts, interactive selections, and key action triggers.
- **AI Accent (`#F59E0B`)**: Reserved for synthesized summaries, smart catalog suggestions, citation generation, and semantic search indicators.
- **Inventory Status Indicators**:
  - `Available`: Success Green (`#16A34A`) paired with soft green fills (`#F0FDF4`).
  - `Checked Out`: Warning Orange (`#EA580C`) with amber-tinted backdrops (`#FFF7ED`).
  - `Not Available / Lost`: Destructive Red (`#DC2626`) with light crimson fills (`#FEF2F2`).
- **Section & Shelf Wayfinding Accents**:
  - `Sci-Fi`: Cobalt Blue (`#3B82F6`)
  - `Mystery`: Crimson Red (`#EF4444`)
  - `History`: Violet (`#8B5CF6`)
  - `Science & Tech`: Emerald (`#10B981`)
  - `Reference`: Slate Grey (`#6B7280`)
  - `Periodicals`: Warm Amber (`#F59E0B`)

All section colors must be applied as hairline left-edge tags, status badges, or subdued micro-tags to prevent visual cacophony across high-density search lists.

## Typography

The typographic hierarchy pairs **Inter** for primary structural readability with **JetBrains Mono** for bibliographic specificity, Library of Congress/Dewey Decimal call numbers, barcode strings, and metadata tags.

- **Headlines & Interface Labels**: Set in `Inter` with snug letter spacing (`-0.01em` to `-0.02em`) to maintain editorial density without sacrificing legibility in multi-column tables.
- **Body & Continuous Copy**: Clean, unembellished `Inter` utilizing tall x-heights for rapid parsing of abstracts, author notes, and patron details.
- **Classification & Machine Data**: Exclusively rendered in `JetBrains Mono`. Call numbers, ISBNs, shelf locations, and copy counts utilize tabular figures to guarantee alignment across dense inventory grids.

## Layout & Spacing

The layout is built upon an 8-point harmonic grid, translating down to 4px subdivisions for micro-alignments, pill tags, and table cell compact modes.

- **Grid System**: A 12-column fluid grid system across desktop viewports (`≥ 1280px`) with dynamic catalog collapse to 8 columns on tablet (`768px - 1279px`) and 4 columns on mobile (`< 768px`).
- **Catalog Density Modes**:
  - *Standard View*: 24px grid gutter, comfortable reading containers capped at 1440px max-width.
  - *High-Density Archival View*: 16px gutter, fluid full-width table canvas with compressed row padding (`8px` vertical cell padding).
- **Shelving Rhythm**: Book cards and catalog drawers leverage precise `space-md` gaps to maintain strict vertical scanning lines. Outer margin safeguards protect edge readability on handheld inventory scanners and mobile devices.

## Elevation & Depth

In alignment with modern scholarly minimalism and ShadCN UI paradigms, visual depth relies on low-contrast outlines, tonal containment, and ultra-subtle ambient drop shadows rather than heavy physical elevation.

- **Base Layer (Surface 0)**: `#FAFAFA` (zinc-50) canvas background.
- **Container Layer (Surface 1)**: Pure white (`#FFFFFF`) card and panel backgrounds bound by a 1px solid border in `#E4E4E7` (zinc-200).
- **Floating Overlays (Surface 2)**: Flyout drawers, context menus, command palettes, and modal sheets carry a faint hairline border (`#E4E4E7`) backed by an ambient shadow: `box-shadow: 0 4px 20px -2px rgba(9, 9, 11, 0.05), 0 2px 6px -1px rgba(9, 9, 11, 0.03)`.
- **Hover Transitions**: Interactive book covers and table rows avoid heavy vertical lifts. Elevation shift is limited to a 1-pixel border tint shift from `zinc-200` to `zinc-400` or a lightened fill transition to `zinc-50`.

## Shapes

The design system maintains a crisp, architectural restraint with a **Soft (Level 1)** roundedness configuration. 

- **Structural Elements (Buttons, Inputs, Cards)**: Default radius is `0.25rem` (4px). This honors the rectangular geometry of physical books, catalog index cards, and institutional card drawers.
- **Large Panels & Modals**: Softly contained with `0.5rem` (8px / `rounded-lg`).
- **Badges & Call Number Indicators**: Strict micro-radii of `0.25rem` or semi-pill structures when communicating status to balance geometric precision with rapid scannability.
- **Cover Image Aspect Ratio**: Fixed strictly to an authentic book proportion of `2:3` with a subtle `4px` corner radius and a 1px inset alpha border to avoid raw visual edges against light backgrounds.

## Components

### Buttons
- **Primary**: Solid `#2563EB` background with `#FFFFFF` text. Micro-radius (4px), 36px standard height (`h-9`), padding `px-4`. Hover state transitions to `#1D4ED8` with zero vertical displacement.
- **Secondary / Outline**: `#FFFFFF` background, `#09090B` text, wrapped in a 1px `#E4E4E7` border. Hover transitions to `#F4F4F5`.
- **AI Assist**: Ghost or soft amber-tinted background (`#FEF3C7`), text `#92400E`, with an amber icon.
- **Destructive**: Bare flat red tint or outline `#DC2626`, transitioning to solid `#DC2626` only upon destructive confirmation.

### Input Fields & Search Bars
- Standard height `h-9` (or `h-11` for the global catalog search bar). Built with `#FFFFFF` background, 1px `#E4E4E7` border, `rounded` (4px), and typography anchored at `text-sm`.
- Focus state reveals a crisp 1px ring in `#2563EB` with zero offset blur.
- Integrated keyboard shortcut tags (`⌘K`) set in `JetBrains Mono` at `text-[10px]` in muted zinc.

### Catalog Book Cards (2:3 Aspect Ratio)
- Fixed container rendering a portrait frame (`aspect-[2/3]`), containing the book jacket, overlaid with an optional status micro-tag on the top-right corner.
- Beneath the cover: Book title in `Inter` semi-bold (`text-sm`), primary author in `text-xs text-zinc-500`, followed by the call number badge set in `JetBrains Mono text-[11px]` on `#F4F4F5`.
- Left-edge hairline accent: Color-coded to match the respective classification section (e.g., 2px vertical indicator on the left side of the card).

### Chips & Classification Badges
- **Status Chips**:
  - `Available`: `#DCFCE7` background, `#15803D` text, dot indicator `#16A34A`.
  - `Checked Out`: `#FFEDD5` background, `#C2410C` text, dot indicator `#EA580C`.
  - `Not Available`: `#FEE2E2` background, `#B91C1C` text, dot indicator `#DC2626`.
- **Call Number Badges**: Neutral light zinc container (`#F4F4F5`), high-contrast mono text (`JetBrains Mono`, `text-[11px]`), 1px `#E4E4E7` border.

### Data Tables (Librarian Inventory View)
- Border-collapsed layout with alternating row backgrounds or crisp 1px bottom border (`#E4E4E7`).
- Dense vertical padding (`py-2` to `py-3`).
- Monospaced alignment for barcodes, Dewey/LC call numbers, copy ratios (`available / total`), and publication years.

### Checkboxes & Radio Buttons
- 16x16px boxes with `rounded-[3px]`, 1px solid `#D4D4D8`.
- Checked state fills with `#2563EB` featuring an optical-center white checkmark. Focus rings follow the primary hairline standard.