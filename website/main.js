const guides = {
  playstation: {
    title: "在 PS5 上设置 DNS",
    path: "设置 → 网络 → 设置 → 设定互联网连接 → 高级设置",
  },
  xbox: {
    title: "在 Xbox 上设置 DNS",
    path: "设置 → 常规 → 网络设置 → 高级设置 → DNS 设置",
  },
  switch: {
    title: "在 Nintendo Switch 上设置 DNS",
    path: "系统设置 → 互联网 → 互联网设置 → 当前网络 → 更改设置",
  },
};

const platformButtons = document.querySelectorAll("[data-platform]");
for (const button of platformButtons) {
  button.addEventListener("click", () => {
    for (const choice of platformButtons)
      choice.setAttribute("aria-pressed", String(choice === button));
    const guide = guides[button.dataset.platform];
    document.querySelector("#guide-title").textContent = guide.title;
    document.querySelector("#guide-path").textContent = guide.path;
  });
}

const copyButton = document.querySelector("#copy-command");
copyButton.addEventListener("click", async () => {
  const command = document.querySelector("#install-command");
  const status = document.querySelector("#copy-status");
  copyButton.disabled = true;
  try {
    await navigator.clipboard.writeText(command.textContent.trim());
    status.textContent = "已复制";
  } catch {
    const selection = window.getSelection();
    const range = document.createRange();
    range.selectNodeContents(command);
    selection.removeAllRanges();
    selection.addRange(range);
    command.parentElement.focus();
    status.textContent = "命令已选中，请按 Ctrl/Cmd+C 或长按复制。";
  } finally {
    copyButton.disabled = false;
  }
});
