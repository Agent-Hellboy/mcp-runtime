(function () {
  async function renderMermaid() {
    if (typeof mermaid === "undefined") {
      return;
    }

    var fallbackSources = [];
    try {
      var response = await fetch(window.location.href, { cache: "no-store" });
      var page = new DOMParser().parseFromString(await response.text(), "text/html");
      fallbackSources = Array.from(
        page.querySelectorAll("pre.mermaid-source > code, pre.mermaid > code"),
      ).map(function (code) {
        return code.textContent.trim();
      });
    } catch (error) {
      fallbackSources = [];
    }

    document
      .querySelectorAll("pre.mermaid-source > code, pre.mermaid > code")
      .forEach(function (code, index) {
        var container = document.createElement("div");
        container.className = "mermaid";
        container.textContent = code.textContent.trim() || fallbackSources[index] || "";
        code.parentElement.replaceWith(container);
      });

    mermaid.initialize({
      startOnLoad: false,
      theme: "base",
      themeVariables: {
        background: "#ffffff",
        fontSize: "17px",
        fontFamily:
          'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
        primaryColor: "#eff6ff",
        primaryBorderColor: "#2563eb",
        primaryTextColor: "#172033",
        secondaryColor: "#ecfdf5",
        secondaryBorderColor: "#047857",
        tertiaryColor: "#f8fafc",
        tertiaryBorderColor: "#94a3b8",
        lineColor: "#334155",
        textColor: "#172033",
        mainBkg: "#ffffff",
        nodeBorder: "#2563eb",
        clusterBkg: "#f8fafc",
        clusterBorder: "#b7c3d2",
        edgeLabelBackground: "#ffffff",
        actorBkg: "#eff6ff",
        actorBorder: "#2563eb",
        actorTextColor: "#172033",
        actorLineColor: "#334155",
        signalColor: "#334155",
        signalTextColor: "#172033",
        labelBoxBkgColor: "#f8fafc",
        labelBoxBorderColor: "#94a3b8",
        labelTextColor: "#172033",
        loopTextColor: "#172033",
        noteBkgColor: "#fef3c7",
        noteBorderColor: "#d97706",
        noteTextColor: "#172033",
        activationBkgColor: "#dbeafe",
        activationBorderColor: "#2563eb",
        actorFontSize: "17px",
        messageFontSize: "16px",
        noteFontSize: "16px",
      },
      flowchart: { useMaxWidth: true, htmlLabels: true },
      sequence: {
        useMaxWidth: true,
        diagramMarginX: 36,
        diagramMarginY: 24,
        boxMargin: 12,
        actorMargin: 72,
        messageMargin: 40,
      },
      securityLevel: "strict",
    });

    var diagrams = Array.from(document.querySelectorAll(".mermaid"));
    for (var index = 0; index < diagrams.length; index += 1) {
      var diagram = diagrams[index];
      var source = diagram.textContent.trim();
      if (!source || diagram.dataset.processed === "true") {
        continue;
      }

      try {
        var rendered = await mermaid.render(
          "mcp-runtime-mermaid-" + index + "-" + Date.now(),
          source,
        );
        diagram.innerHTML = rendered.svg;
        diagram.dataset.processed = "true";
        if (rendered.bindFunctions) {
          rendered.bindFunctions(diagram);
        }
      } catch (error) {
        console.error("Mermaid render failed", error);
      }
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", renderMermaid);
  } else {
    renderMermaid();
  }
})();
