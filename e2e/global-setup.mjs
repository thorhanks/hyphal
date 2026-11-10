import { execFileSync } from "node:child_process";
import { binary, repo } from "./helpers.mjs";

// One dev build for the whole run: --dev serves window.__hyphal and --seed.
export default function setup() {
  execFileSync("go", ["build", "-tags", "dev", "-o", binary, "./cmd/hyphal"], { cwd: repo, stdio: "inherit" });
}
