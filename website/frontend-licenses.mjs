// Include the locked production packages' license texts in the static delivery.
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
const root = path.dirname(new URL(import.meta.url).pathname);
const packages = execFileSync("npm", ["ls", "--omit=dev", "--parseable"], {
  cwd: root,
  encoding: "utf8",
})
  .trim()
  .split("\n")
  .slice(1);
console.log(
  "Frontend dependencies: licenses for the packages in website/package-lock.json.\n",
);
for (const directory of packages) {
  const metadata = JSON.parse(
    fs.readFileSync(path.join(directory, "package.json"), "utf8"),
  );
  const license = fs
    .readdirSync(directory)
    .find((name) => /^licen[sc]e(?:\.(?:md|txt))?$/i.test(name));
  if (!license) throw new Error("Missing license text: " + metadata.name);
  console.log(
    `\n--- ${metadata.name} ${metadata.version} (${metadata.license}) ---\n` +
      fs.readFileSync(path.join(directory, license), "utf8"),
  );
  const notices = fs
    .readdirSync(directory)
    .find((name) => /^thirdpartynotices\.txt$/i.test(name));
  if (notices)
    console.log(fs.readFileSync(path.join(directory, notices), "utf8"));
}
