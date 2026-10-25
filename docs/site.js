(() => {
  const isRussian = /^ru(?:-|$)/i.test(navigator.language || "");
  if (!isRussian) return;

  document.documentElement.lang = "ru";
  document.title = "OSTOM — восстановление почты Outlook";
  const description = document.querySelector('meta[name="description"]');
  if (description) {
    description.content = "OSTOM — read-only утилита для восстановления читаемых писем из повреждённых Outlook PST и OST файлов.";
  }

  document.querySelectorAll("[data-ru]").forEach((element) => {
    element.textContent = element.dataset.ru;
  });

  document.querySelectorAll('a[href*="/docs/"]').forEach((link) => {
    link.href = link.href.replace("THUNDERBIRD.md", "THUNDERBIRD.ru.md")
      .replace("FORMAT.md", "FORMAT.ru.md")
      .replace("TODO.md", "TODO.ru.md");
  });
})();
