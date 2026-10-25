import { type ReactNode, useId, useState } from "react";

import { setTheme, type Theme, useTheme } from "@/app/theme-toggle";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";

/** Settings (design.md routes): theme, terminal, editor and lab preferences, and your data. Only the theme is kept
 * yet; the others are what the terminal and editor features will read. */
export function Settings() {
  const theme = useTheme();
  return (
    <div className="max-w-3xl">
      <h1 className="font-serif text-4xl">Settings</h1>
      <p className="mt-2 text-ink-soft">Kept on this machine, for every course.</p>

      <Group title="Appearance">
        <Row label="Theme" hint="Night shift is the dark panel; System follows your desktop.">
          <RadioGroup
            value={theme}
            onValueChange={(v) => {
              setTheme(v as Theme);
            }}
            className="flex flex-wrap gap-4"
            aria-label="Theme"
          >
            {(
              [
                ["system", "System"],
                ["light", "Day"],
                ["dark", "Night shift"],
              ] as const
            ).map(([value, name]) => (
              <label key={value} className="flex items-center gap-2">
                <RadioGroupItem value={value} className="border-ink" />
                {name}
              </label>
            ))}
          </RadioGroup>
        </Row>
      </Group>

      <Group title="Terminal">
        <Row label="Font size">
          <Pick label="Font size" values={["13", "14", "15", "16", "18"]} initial="15" unit="px" />
        </Row>
        <Toggle label="Blinking cursor" initial />
        <Toggle label="Copy on select" hint="Select text in the terminal to copy it, as in most Linux terminals." />
      </Group>

      <Group title="Editor">
        <Row label="Key bindings">
          <Pick label="Key bindings" values={["VS Code", "Vim"]} initial="VS Code" />
        </Row>
        <Row label="Tab size">
          <Pick label="Tab size" values={["2", "4"]} initial="2" unit="spaces" />
        </Row>
        <Toggle
          label="Save as you type"
          hint="Every change reaches the lab's files a second after you stop typing."
          initial
        />
        <Toggle label="Minimap" />
      </Group>

      <Group title="Labs">
        <Toggle label="Ask before abandoning a lab" hint="Abandoning tears the lab down and heals the links." initial />
        <Toggle label="Ask before opening a hint" hint="Each hint takes 10 % off the lab's XP." initial />
        <Toggle
          label="Run commands with a click"
          hint="The ▶ on a command types it in the terminal and presses Enter."
          initial
        />
      </Group>

      <Group title="Your data">
        <Row label="Progress" hint="Every attempt, pass, note and badge, as one JSON file.">
          <Button variant="outline" className="bg-paper">
            Export progress
          </Button>
        </Row>
        <Row label="Start over" hint="Deletes your progress on this machine. Courses and labs stay.">
          <Button variant="outline" className="border-red bg-paper text-red hover:bg-red hover:text-paper">
            Delete my progress
          </Button>
        </Row>
      </Group>
    </div>
  );
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="mt-10">
      <h2 className="border-b-2 border-ink pb-2 text-xl font-extrabold">{title}</h2>
      <div className="divide-y divide-rule">{children}</div>
    </section>
  );
}

function Row({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-8 gap-y-3 py-4">
      <div className="min-w-48 flex-1">
        <p className="font-semibold">{label}</p>
        {hint ? <p className="max-w-[55ch] text-sm text-ink-soft">{hint}</p> : null}
      </div>
      {children}
    </div>
  );
}

function Toggle({ label, hint, initial = false }: { label: string; hint?: string; initial?: boolean }) {
  const [on, setOn] = useState(initial);
  const id = useId();
  return (
    <div className="flex items-center justify-between gap-8 py-4">
      <div>
        <label htmlFor={id} className="font-semibold">
          {label}
        </label>
        {hint ? <p className="max-w-[55ch] text-sm text-ink-soft">{hint}</p> : null}
      </div>
      <Switch id={id} checked={on} onCheckedChange={setOn} />
    </div>
  );
}

function Pick({ label, values, initial, unit }: { label: string; values: string[]; initial: string; unit?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <Select value={value} onValueChange={setValue}>
      <SelectTrigger aria-label={label} className="w-36 bg-paper">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {values.map((v) => (
          <SelectItem key={v} value={v}>
            {unit ? `${v} ${unit}` : v}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
