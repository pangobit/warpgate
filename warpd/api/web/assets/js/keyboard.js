document.documentElement.classList.toggle(
  "dark",
  localStorage.getItem("warpgate-theme") === "dark",
);

document.addEventListener("click", function (event) {
  const button = event.target.closest("[data-theme-toggle]");
  if (!button) {
    return;
  }
  const dark = !document.documentElement.classList.contains("dark");
  document.documentElement.classList.toggle("dark", dark);
  localStorage.setItem("warpgate-theme", dark ? "dark" : "light");
});
