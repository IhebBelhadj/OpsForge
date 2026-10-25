import type { Course, ItemRef, ItemType } from "@/api/client";

export interface Placed {
  item: ItemRef;
  section: number;
  /** "2.3": section 2, its third item (as Udemy numbers them) */
  number: string;
}

/** A course's items in order, each with its section and number. */
export function placed(course: Course): Placed[] {
  return course.sections.flatMap((s, si) =>
    s.items.map((item, ii) => ({ item, section: si, number: `${String(si + 1)}.${String(ii + 1)}` })),
  );
}

export const typeName: Record<ItemType, string> = {
  lecture: "Lecture",
  quiz: "Quiz",
  lab: "Lab",
  challenge: "Challenge",
};
