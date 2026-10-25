/** "8 min", "1 h", "6 h 10 min". */
export function duration(minutes: number): string {
  if (minutes < 60) return `${String(minutes)} min`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m ? `${String(h)} h ${String(m)} min` : `${String(h)} h`;
}

/** "1 lab", "3 labs"; irregular plurals passed in. */
export function count(n: number, one: string, many = `${one}s`): string {
  return `${String(n)} ${n === 1 ? one : many}`;
}

export const levelName = { beginner: "Beginner", intermediate: "Intermediate", advanced: "Advanced" } as const;
