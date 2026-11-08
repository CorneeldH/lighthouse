export type Tone = 'purple' | 'orange' | 'blue' | 'grey' | 'yellow' | 'navy' | 'green'

export const toneClass: Record<Tone, string> = {
  // Icon contrast vs tile is >= 3:1 in both themes; dark tints take dark ink.
  purple: 'bg-lh-purple text-white dark:text-[#0b1020]',
  orange: 'bg-lh-orange text-[#14213d] dark:text-[#0b1020]',
  blue: 'bg-lh-blue text-white dark:text-[#0b1020]',
  grey: 'bg-lh-grey text-white dark:text-[#0b1020]',
  yellow: 'bg-lh-yellow text-[#14213d]',
  navy: 'bg-lh-navy text-white ring-1 ring-border',
  green: 'bg-lh-green text-white dark:text-[#0b1020]',
}
