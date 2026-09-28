// Small progressive-enhancement layer over HTMX. The data tabs are plain HTML
// forms; this file adds delete confirmations there and runs the chat page.
(function () {
  "use strict";

  const $ = (sel, root = document) => root.querySelector(sel);

  // Forms (or submit buttons) marked data-confirm ask first. Used for deletes.
  document.addEventListener("submit", (e) => {
    const el = e.submitter?.dataset.confirm ? e.submitter : e.target;
    const msg = el.dataset?.confirm;
    if (msg && !window.confirm(msg)) e.preventDefault();
  });

  // Only one inline edit form open at a time; Escape closes it.
  document.addEventListener("toggle", (e) => {
    if (!e.target.matches?.("details.edit") || !e.target.open) return;
    document.querySelectorAll("details.edit[open]").forEach((d) => {
      if (d !== e.target) d.open = false;
    });
    e.target.querySelector("input:not([type=hidden]), textarea")?.focus();
  }, true);
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") document.querySelectorAll("details.edit[open]").forEach((d) => (d.open = false));
  });

  // On phones the tab strip scrolls sideways; bring the current tab into view
  // so pages further along (Diet, Supplements…) don't show a strip of others.
  document.addEventListener("DOMContentLoaded", () => {
    const tabs = $(".tabs"),
      active = $(".tab.active");
    if (tabs && active && tabs.scrollWidth > tabs.clientWidth) {
      tabs.scrollLeft = active.offsetLeft - tabs.offsetLeft - (tabs.clientWidth - active.offsetWidth) / 2;
    }
  });

  // ---- Chat ----
  const scroll = () => {
    const box = $("#chat-scroll");
    if (box) box.scrollTop = box.scrollHeight;
  };
  document.addEventListener("DOMContentLoaded", scroll);
  // A prompt prefilled from another page (?prompt=...) sizes the composer to fit.
  document.addEventListener("DOMContentLoaded", () => {
    const input = $("#chat-input");
    if (input && input.value) autosize(input);
  });

  // Grow the composer with its content (up to the CSS max-height).
  function autosize(el) {
    el.style.height = "auto";
    el.style.height = el.scrollHeight + "px";
  }
  function sendChat() {
    const form = $("#chat-form");
    if (!form || form.dataset.busy === "1" || form.dataset.disabled === "1") return;
    if (attachments.some((a) => a.busy)) return; // still downscaling a photo
    if (window.htmx) window.htmx.trigger(form, "submit");
  }

  // ---- Photo attachments ----
  // Photos are downscaled in the browser (longest edge 1568px, JPEG) before
  // upload: phone photos are often 4-8 MB, the API caps images at 5 MB, and
  // anything larger than ~1.15 megapixels is resized server-side anyway. The
  // resized files are written back into the hidden <input type=file> so HTMX
  // uploads them with the rest of the form.
  const MAX_ATTACH = 4;
  const MAX_EDGE = 1568;
  const attachments = []; // {id, file, url, busy}
  let attachSeq = 0;

  function syncFileInput() {
    const input = $("#chat-files");
    if (!input) return;
    try {
      const dt = new DataTransfer();
      attachments.forEach((a) => {
        if (!a.busy && a.file) dt.items.add(a.file);
      });
      input.files = dt.files;
    } catch (e) {
      /* very old browsers: leave the picker's own files in place */
    }
  }

  function renderPreviews() {
    const box = $("#chat-previews");
    if (!box) return;
    box.innerHTML = "";
    box.hidden = attachments.length === 0;
    attachments.forEach((a) => {
      const wrap = document.createElement("div");
      wrap.className = "chat-preview" + (a.busy ? " busy" : "");
      const img = document.createElement("img");
      img.src = a.url;
      img.alt = "";
      const rm = document.createElement("button");
      rm.type = "button";
      rm.className = "remove";
      rm.title = "Remove";
      rm.setAttribute("aria-label", "Remove photo");
      rm.innerHTML =
        '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>';
      rm.addEventListener("click", () => removeAttachment(a.id));
      wrap.append(img, rm);
      box.appendChild(wrap);
    });
  }

  function removeAttachment(id) {
    const i = attachments.findIndex((a) => a.id === id);
    if (i < 0) return;
    URL.revokeObjectURL(attachments[i].url);
    attachments.splice(i, 1);
    renderPreviews();
    syncFileInput();
  }

  function clearAttachments() {
    attachments.forEach((a) => URL.revokeObjectURL(a.url));
    attachments.length = 0;
    renderPreviews();
    syncFileInput();
  }

  function loadImage(url) {
    return new Promise((resolve, reject) => {
      const img = new Image();
      img.onload = () => resolve(img);
      img.onerror = reject;
      img.src = url;
    });
  }

  // Returns a File no larger than MAX_EDGE on its longest side. Small JPEG/PNG/
  // WebP/GIF files pass through untouched; everything else (including HEIC on
  // browsers that can decode it) is re-encoded as JPEG.
  async function shrinkImage(file, url) {
    const passthrough = ["image/jpeg", "image/png", "image/webp", "image/gif"];
    let img;
    try {
      img = await loadImage(url);
    } catch (e) {
      return file; // undecodable here; let the server decide
    }
    const w = img.naturalWidth || img.width;
    const h = img.naturalHeight || img.height;
    const scale = Math.min(1, MAX_EDGE / Math.max(w, h));
    if (scale === 1 && passthrough.includes(file.type) && file.size <= 1.5 * 1024 * 1024) return file;
    if (file.type === "image/gif" && file.size <= 4 * 1024 * 1024) return file; // keep animation
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(w * scale);
    canvas.height = Math.round(h * scale);
    const ctx = canvas.getContext("2d");
    if (!ctx) return file;
    ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise((res) => canvas.toBlob(res, "image/jpeg", 0.86));
    if (!blob) return file;
    const name = (file.name || "photo").replace(/\.[^.]+$/, "") + ".jpg";
    return new File([blob], name, { type: "image/jpeg" });
  }

  async function addFiles(files) {
    const form = $("#chat-form");
    if (!form || form.dataset.disabled === "1") return;
    for (const file of Array.from(files || [])) {
      if (!file || (!file.type.startsWith("image/") && !/\.(heic|heif)$/i.test(file.name || ""))) continue;
      if (attachments.length >= MAX_ATTACH) {
        showChatNotice("You can attach up to " + MAX_ATTACH + " photos per message.");
        break;
      }
      const entry = { id: ++attachSeq, file: null, url: URL.createObjectURL(file), busy: true };
      attachments.push(entry);
      renderPreviews();
      try {
        entry.file = await shrinkImage(file, entry.url);
      } catch (e) {
        entry.file = file;
      }
      entry.busy = false;
      if (attachments.includes(entry)) {
        renderPreviews();
        syncFileInput();
      }
    }
    $("#chat-input")?.focus();
  }

  // Brief inline notice under the composer (limits, upload errors).
  function showChatNotice(text) {
    const hint = $(".composer-hint");
    if (!hint) return;
    if (!hint.dataset.orig) hint.dataset.orig = hint.textContent;
    hint.textContent = text;
    hint.classList.add("chat-err");
    clearTimeout(showChatNotice.t);
    showChatNotice.t = setTimeout(() => {
      hint.textContent = hint.dataset.orig;
      hint.classList.remove("chat-err");
    }, 4000);
  }

  document.addEventListener("click", (e) => {
    if (e.target.closest("#chat-attach") || e.target.closest(".starter-photo")) {
      const input = $("#chat-files");
      if (input && !input.disabled) input.click();
    }
  });
  document.addEventListener("change", (e) => {
    // Styled file inputs (.filepick): show the chosen filename(s) next to the
    // custom button.
    if (e.target.matches?.('.filepick input[type="file"]')) {
      const label = e.target.closest(".filepick")?.querySelector(".filepick-name");
      if (label) {
        const files = Array.from(e.target.files || []);
        label.textContent =
          files.length === 0 ? "" :
          files.length === 1 ? files[0].name :
          files.length + " files selected";
      }
      return;
    }
    if (e.target.id !== "chat-files") return;
    // The picker's own selection is replaced by the downscaled copies once
    // they're ready, so grab the originals first.
    const picked = Array.from(e.target.files || []);
    syncFileInput();
    addFiles(picked);
  });
  // Paste a screenshot or a copied image straight into the composer.
  document.addEventListener("paste", (e) => {
    if (e.target.id !== "chat-input") return;
    const items = Array.from(e.clipboardData?.items || []);
    const files = items.filter((it) => it.kind === "file" && it.type.startsWith("image/")).map((it) => it.getAsFile());
    if (files.length) {
      e.preventDefault();
      addFiles(files);
    }
  });
  // Drag a photo onto the composer.
  document.addEventListener("dragover", (e) => {
    const c = e.target.closest?.("#chat-form");
    if (!c) return;
    e.preventDefault();
    c.classList.add("dragover");
  });
  document.addEventListener("dragleave", (e) => {
    const c = e.target.closest?.("#chat-form");
    if (c && !c.contains(e.relatedTarget)) c.classList.remove("dragover");
  });
  document.addEventListener("drop", (e) => {
    const c = e.target.closest?.("#chat-form");
    if (!c) return;
    e.preventDefault();
    c.classList.remove("dragover");
    addFiles(e.dataTransfer?.files);
  });
  document.addEventListener("input", (e) => {
    if (e.target.id === "chat-input") autosize(e.target);
  });
  document.addEventListener("keydown", (e) => {
    if (e.target.id === "chat-input" && e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      sendChat();
    }
  });
  // Starter prompts on an empty chat send immediately.
  document.addEventListener("click", (e) => {
    const starter = e.target.closest(".starter");
    if (!starter || starter.classList.contains("starter-photo")) return;
    const input = $("#chat-input");
    input.value = starter.textContent.trim();
    sendChat();
  });

  document.body.addEventListener("htmx:beforeRequest", (e) => {
    if (e.target.id !== "chat-form") return;
    const form = e.target;
    const input = $("#chat-input");
    const col = $("#chat-col");
    const val = (input.value || "").trim();
    const ready = attachments.filter((a) => !a.busy && a.file);
    if ((!val && ready.length === 0) || form.dataset.busy === "1" || attachments.some((a) => a.busy)) {
      e.preventDefault();
      return;
    }
    form.dataset.busy = "1";
    form.dataset.lastText = val;
    const empty = col.querySelector(".chat-empty");
    if (empty) empty.remove();
    // Show the user's message right away. The server response re-renders it
    // authoritatively and appends its own polling "thinking" placeholder, so we
    // don't add a pending bubble here (that now comes from the server and keeps
    // polling until the background reply lands).
    const mine = document.createElement("div");
    mine.className = "bubble user optimistic";
    if (ready.length) {
      const strip = document.createElement("div");
      strip.className = "bubble-images";
      ready.forEach((a) => {
        const img = document.createElement("img");
        img.src = a.url;
        img.alt = "";
        strip.appendChild(img);
      });
      mine.appendChild(strip);
    }
    mine.appendChild(document.createTextNode(val));
    col.appendChild(mine);
    scroll();
    // Clear after htmx has serialized the form. Object URLs stay alive until
    // the optimistic bubble is replaced by the server's copy.
    setTimeout(() => {
      input.value = "";
      autosize(input);
      attachments.length = 0;
      renderPreviews();
      syncFileInput();
    }, 0);
  });

  document.body.addEventListener("htmx:afterRequest", (e) => {
    if (e.target.id !== "chat-form") return;
    e.target.dataset.busy = "";
    if (!e.detail.successful) {
      // The POST itself failed: a rejected upload (4xx with a message) or an
      // unreachable server. Drop the optimistic bubble, restore the text so
      // nothing is lost, and say what went wrong.
      const xhr = e.detail.xhr;
      const msg =
        xhr && xhr.status >= 400 && xhr.status < 500 && xhr.responseText && xhr.responseText.length < 300
          ? xhr.responseText.trim()
          : "Couldn't reach the server. Try again.";
      document.querySelectorAll("#chat-col .optimistic").forEach((el) => {
        el.querySelectorAll("img").forEach((img) => URL.revokeObjectURL(img.src));
        el.remove();
      });
      const input = $("#chat-input");
      if (input && !input.value) {
        input.value = e.target.dataset.lastText || "";
        autosize(input);
      }
      showChatNotice(msg);
    } else {
      // Success: the server appended the authoritative user bubble and a polling
      // placeholder, so drop our optimistic copy of the user's message.
      document.querySelectorAll("#chat-col .optimistic").forEach((el) => {
        el.querySelectorAll("img").forEach((img) => URL.revokeObjectURL(img.src));
        el.remove();
      });
    }
    $("#chat-input")?.focus();
    scroll();
  });

  // On phones the conversation list slides over the page and covers its own
  // toggle button, so a tap anywhere outside the open panel closes it.
  document.addEventListener("click", (e) => {
    const side = $("#chat-side");
    if (!side || !side.classList.contains("open")) return;
    if (side.contains(e.target) || e.target.closest(".side-toggle")) return;
    side.classList.remove("open");
  });

  // Keep the header title in sync with the sidebar (new chats get a title
  // from the server after the first reply; renames update it too).
  function syncTitle() {
    const active = $(".thread.active .thread-link");
    const h = $(".chat-title");
    if (active && h) {
      h.textContent = active.textContent;
      document.title = "Tandem — " + active.textContent;
    }
  }
  document.body.addEventListener("htmx:oobAfterSwap", syncTitle);
  document.body.addEventListener("htmx:afterSwap", (e) => {
    if (e.target.id === "thread-list") syncTitle();
  });

  document.addEventListener("click", (e) => {
    const btn = e.target.closest(".thread-rename");
    if (!btn) return;
    const title = window.prompt("Rename conversation", btn.dataset.title);
    if (!title || !title.trim() || !window.htmx) return;
    window.htmx.ajax("POST", "/threads/" + btn.dataset.id + "/rename", {
      target: "#thread-list",
      swap: "outerHTML",
      values: { title: title.trim(), current: btn.dataset.current },
    });
  });

  // When the API key changes, the chat's enabled state (chat input, empty
  // note, "key needed" tag) needs to flip. Reload after a short beat so the
  // user sees the inline success message first.
  document.body.addEventListener("settings-changed", () => {
    setTimeout(() => window.location.reload(), 700);
  });

})();
