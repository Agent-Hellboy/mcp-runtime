document.addEventListener("DOMContentLoaded", function () {
  if (typeof mermaid !== "undefined") {
    mermaid.initialize({
      startOnLoad: true,
      theme: "default",
      flowchart: { useMaxWidth: true, htmlLabels: true },
      sequence: { useMaxWidth: true },
      securityLevel: "loose",
    });
  }
});
