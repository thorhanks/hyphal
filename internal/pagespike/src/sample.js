export const sample = `# Lighthouse restoration

Survey the **keeper's cottage** before the *autumn* storms, and keep the ~~old~~ ==new== estimate with the \`roof\` notes.

## Roof work

- Price the slate
- Book the ferry for March
  - Check the tide tables
- [ ] Call the surveyor
- [x] Find the old drawings

1. Strip the old lead
2. Replace the flashing

> The keeper's log says the north wall
> took the worst of the 1953 storm.

| Item | Cost | Notes |
| --- | --- | --- |
| **Slate** | 4,200 | Welsh |
| Lead | 1,150 | See [supplier](https://example.com/lead) |

\`\`\`go
func estimate(items []Item) int {
	total := 0
	return total
}
\`\`\`

Read the [harbor survey](https://example.com/survey "Survey") first.

---

Last paragraph after the rule.
`;

export function longPage(lines) {
  const out = ["# Long page", ""];
  for (let i = 0; out.length < lines; i++) {
    switch (i % 5) {
      case 0: out.push(`## Section ${i}`, ""); break;
      case 1: out.push(`Paragraph ${i} with **bold** and *italic* text, a \`code\` span and a [link](https://example.com/${i}) that wraps across the reading column on a normal window, so line heights vary.`, ""); break;
      case 2: out.push(`- Item ${i}`, `- [ ] Task ${i}`, `- Another item`, ""); break;
      case 3: out.push("| A | B |", "| --- | --- |", `| ${i} | **two** |`, ""); break;
      default: out.push("```js", `const n = ${i};`, "```", ""); break;
    }
  }
  return out.join("\n");
}
