function resetDeployForm(form) {
  form.dataset.submitting = "false";
  const button = form.querySelector("[data-deploy-button]");
  const label = form.querySelector("[data-deploy-label]");
  const loading = form.querySelector("[data-deploy-loading]");
  if (button) {
    button.disabled = false;
    button.removeAttribute("aria-busy");
  }
  if (label) {
    label.hidden = false;
  }
  if (loading) {
    loading.hidden = true;
  }
}

document.addEventListener("submit", function (event) {
  const form = event.target.closest("[data-deploy-form]");
  if (!form) {
    return;
  }
  if (form.dataset.submitting === "true") {
    event.preventDefault();
    return;
  }
  form.dataset.submitting = "true";
  const button = form.querySelector("[data-deploy-button]");
  const label = form.querySelector("[data-deploy-label]");
  const loading = form.querySelector("[data-deploy-loading]");
  if (button) {
    button.disabled = true;
    button.setAttribute("aria-busy", "true");
  }
  if (label) {
    label.hidden = true;
  }
  if (loading) {
    loading.hidden = false;
  }
});

window.addEventListener("pageshow", function () {
  document.querySelectorAll("[data-deploy-form]").forEach(resetDeployForm);
});
