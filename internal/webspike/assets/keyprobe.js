// Human check for AC3: shows which §6.4 bindings reach the page in this browser.
const wanted = ["Control+q","Control+p","Control+k","Control+z","Control+y","Control+s","Control+w","Control+a","Control+e","Control+u","Control+c","Control+x","Control+f","Control+Enter","Control+Shift+ArrowUp","Control+Shift+ArrowDown","Control+Home","Control+End","Control+ArrowLeft","Control+ArrowRight","Control+Backspace","F1","Tab","Shift+Tab","Escape","Alt+b","Alt+f","Alt+ArrowLeft","Alt+ArrowRight","Alt+Backspace","PageUp","PageDown"];
const list = document.getElementById("keys");
const items = new Map();
for (const k of wanted) {
  const li = document.createElement("li");
  li.textContent = k + ": not seen";
  list.append(li);
  items.set(k, li);
}
function name(e) {
  const parts = [];
  if (e.ctrlKey) parts.push("Control");
  if (e.altKey) parts.push("Alt");
  if (e.shiftKey) parts.push("Shift");
  parts.push(e.key.length === 1 ? e.key.toLowerCase() : e.key);
  return parts.join("+");
}
addEventListener("keydown", (e) => {
  e.preventDefault();
  const li = items.get(name(e));
  if (li) li.textContent = name(e) + ": reached the page";
});
