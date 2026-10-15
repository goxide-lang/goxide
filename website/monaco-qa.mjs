// Drive the real editor through its keyboard and copy control, without product test hooks.
export function view(page, label) {
  return page
    .locator("section.file-view")
    .filter({
      has: page
        .locator(".view-toolbar strong")
        .filter({ hasText: new RegExp("^" + label + "$") }),
    });
}
export async function write(page, source) {
  const pane = view(page, "源码");
  await pane.getByRole("textbox").waitFor({ state: "attached" });
  await page.evaluate((text) => navigator.clipboard.writeText(text), source);
  await pane.getByRole("textbox").focus();
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.press("ControlOrMeta+V");
  await page.evaluate(
    () =>
      new Promise((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(resolve)),
      ),
  );
}
export async function read(page, label) {
  const button = view(page, label).getByRole("button", {
    name: "复制文件",
    exact: true,
  });
  if (await button.isDisabled()) return "";
  await button.click();
  await page.waitForFunction(() =>
    navigator.clipboard.readText().then(() => true),
  );
  return page.evaluate(() => navigator.clipboard.readText());
}
export async function done(page) {
  await page.waitForFunction(
    () => document.querySelector("#status")?.textContent.startsWith("转译完成"),
    null,
    { timeout: 60000 },
  );
}
