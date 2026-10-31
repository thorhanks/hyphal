import { execFileSync } from "node:child_process";

export default function setup() {
  execFileSync("npm", ["run", "-s", "build"], { stdio: "inherit" });
  execFileSync("go", ["build", "-o", "webspike", "."], { stdio: "inherit" });
}
