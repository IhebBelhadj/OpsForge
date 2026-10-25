import { Progress } from "@/components/ui/progress";
import { topicProgress } from "@/mock";

/** AlgoMonster's pattern progress: each topic with how many of its labs and challenges are passed. */
export function TopicProgress({ onPick }: { onPick?: (topic: string) => void }) {
  return (
    <section aria-labelledby="by-topic" className="rounded-lg border border-rule bg-surface p-5">
      <h2 id="by-topic" className="font-semibold">
        By topic
      </h2>
      <ul className="mt-3 space-y-2.5">
        {topicProgress().map((t) => (
          <li key={t.topic}>
            <p className="flex justify-between text-sm">
              {onPick ? (
                <button
                  type="button"
                  className="underline-offset-4 hover:underline"
                  onClick={() => {
                    onPick(t.topic);
                  }}
                >
                  {t.topic}
                </button>
              ) : (
                <span>{t.topic}</span>
              )}
              <span className="text-ink-soft">
                {t.done}/{t.total}
              </span>
            </p>
            <Progress
              value={(t.done / t.total) * 100}
              className="mt-1.5 h-1"
              aria-label={`${t.topic}: ${String(t.done)} of ${String(t.total)}`}
            />
          </li>
        ))}
      </ul>
    </section>
  );
}
