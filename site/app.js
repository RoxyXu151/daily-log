/* ==========================================================================
   Personal Daily Log · Interactive Showcase Logic
   - Appearance & Oklab room color-temperature switcher (Light/Dark + 4 Accents)
   - Automatic Light/Dark screenshot synchronization
   - Interactive Day Ribbon evidence -> topic inspector
   - Workbench & Phone screen switchers + Lightbox zoom
   - Ecosystem category filter & Quickstart copy-to-clipboard toast
   ========================================================================== */

(() => {
  const root = document.documentElement;
  const STORAGE_THEME = "daily-log-site-theme";
  const STORAGE_ACCENT = "daily-log-site-accent";

  const ribbonTopics = [
    {
      id: "t1",
      period: "上午 · 09:00–10:40",
      category: "科研",
      catColor: "var(--cat-research)",
      title: "整理论文第三章论证结构",
      charCount: "11 / 28 字",
      summary: "定下反例与数据来源的对应关系，改写两段核心推导，并留下一条待实验验证的边界假设。",
      ruleNote: "合并 3 轮 Codex 对话与 Zotero 论文标注 · 保留起止证据",
      evidence: [
        {
          source: "codex · 直接记录",
          time: "09:12 +08:00",
          text: "围绕第三章定理 2 的反例构造与数据集引用口径展开 3 轮推导修订（codex://threads/1）"
        },
        {
          source: "zotero · 文献标注",
          time: "09:48 +08:00",
          text: "新建 4 处 PDF 高亮标注并关联章节笔记，自动归入同日阅读会话（users/0/items/8K2A）"
        }
      ]
    },
    {
      id: "t2",
      period: "下午 · 13:30–15:10",
      category: "课程",
      catColor: "var(--cat-course)",
      title: "一致性哈希分片模拟",
      charCount: "9 / 28 字",
      summary: "完成《分布式系统》课程实验中的虚拟节点环实现，跑通节点扩容与缩容时的键迁移回归测试。",
      ruleNote: "过滤 4 条简单语法查询 · 仅保留实质实现与测试结论",
      evidence: [
        {
          source: "codex · 会话摘录",
          time: "13:42 +08:00",
          text: "排查 150 虚拟节点下的负载标准差并补充迁移比例断言测试"
        },
        {
          source: "github · 官方 API",
          time: "15:04 +08:00",
          text: "推送 3 次提交至 dist-sys-lab：实现 ring_rebalance_test.go 并通过本地验证"
        }
      ]
    },
    {
      id: "t3",
      period: "下午 · 15:30–16:20",
      category: "职业",
      catColor: "var(--cat-career)",
      title: "简历项目描述量化改写",
      charCount: "10 / 28 字",
      summary: "梳理三段核心工程经历，将笼统职责描述替换为可核验的吞吐、延迟与测试覆盖指标。",
      ruleNote: "关联 Todoist 已完成事件与 Obsidian 本地笔记修改",
      evidence: [
        {
          source: "todoist · 已完成任务",
          time: "15:58 +08:00",
          text: "完成任务「改写个人项目技术要点与量化结果」（todoist://completed/94812）"
        },
        {
          source: "obsidian · 本地文件",
          time: "16:14 +08:00",
          text: "编辑笔记 career/resume-2026-zh.md（自动排除日志导出目录）"
        }
      ]
    },
    {
      id: "t4",
      period: "晚上 · 20:00–21:15",
      category: "科研",
      catColor: "var(--cat-research)",
      title: "组会周报与下周实验排期",
      charCount: "11 / 28 字",
      summary: "汇总本周消融实验结果表格，明确下周三组对比基线参数与服务器跑批顺序。",
      ruleNote: "区分日历计划与实际完成事项 · 不把日程时长冒充活跃用时",
      evidence: [
        {
          source: "calendar · 日程记录",
          time: "20:00 +08:00",
          text: "Google Calendar 周期日程实例「课题组双周讨论与排期」（primary/evt_20261124）"
        },
        {
          source: "todoist · 已完成任务",
          time: "21:10 +08:00",
          text: "完成任务「归档本周实验图表并同步共享表格」"
        }
      ]
    }
  ];

  const workbenchViews = {
    dashboard: {
      file: "dashboard-wide",
      title: "今日工作台（Bento 概览与全天时间轴）",
      desc: "一屏聚合今日日期、AI 提炼事项数、原始证据、来源健康状态与 Markdown / Sheets / Excel 同步结果。"
    },
    journal: {
      file: "journal-expanded",
      title: "日记总表（三时段事项展开与分类计数）",
      desc: "默认按日期概览全天精华与分类分钟数；点击任意日期平滑展开上午、下午、晚上事项，对话原文留在证据层。"
    },
    editor: {
      file: "editor",
      title: "宽屏日记编辑器（正文修改与原始证据并排核对）",
      desc: "左侧分时段编辑上午／下午／晚上与本人补充，右侧同屏查阅关联证据、切换预览或比对历史版本。"
    },
    sources: {
      file: "sources-catalog",
      title: "来源管理与标准接入目录",
      desc: "内置 Zotero、Todoist、Codex、Calendar 与常见研发笔记服务目录；填入令牌即可测试连接并查看覆盖窗口。"
    },
    categories: {
      file: "categories",
      title: "动态分类与简称管理",
      desc: "自由新增、排序、设置两字简称或重命名分类；保存后历史事件、手工分类覆盖与表格图例同步迁移。"
    },
    models: {
      file: "models-form",
      title: "多模型卡片管理（本地与云端 OpenAI 兼容协议）",
      desc: "同时保存本机无密钥模型与云端服务，一键拉取官方模型列表、测试连通并切换全局默认模型。"
    },
    schedule: {
      file: "schedule",
      title: "日志更新与系统定时任务",
      desc: "设置北京时间归档时刻、回查天数与纳入事件类型，一键确认安装 Linux systemd 或 Windows 计划任务。"
    }
  };

  const phoneViews = {
    "dashboard-phone": "移动端工作台：窄屏自适应单列卡片与顶栏导航胶囊",
    "phone-journal": "移动端日记列表：触控滚动、按日期状态筛选与事项查阅",
    "phone-editor": "移动端日记编辑：适配中文软键盘长文输入、断网原页重试与多端冲突对照"
  };

  let currentTheme = localStorage.getItem(STORAGE_THEME) ||
    (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  let currentAccent = localStorage.getItem(STORAGE_ACCENT) || "default";
  let currentWorkbench = "dashboard";
  let currentPhone = "dashboard-phone";
  let toastTimer = null;

  function showToast(message) {
    const toast = document.getElementById("toast-band");
    const text = document.getElementById("toast-text");
    if (!toast || !text) return;
    text.textContent = message;
    toast.classList.add("visible");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => toast.classList.remove("visible"), 2200);
  }

  function syncThemeImages() {
    const prefix = currentTheme === "dark" ? "dark" : "light";
    document.querySelectorAll("img[data-shot]").forEach((img) => {
      const shot = img.getAttribute("data-shot");
      img.src = `./assets/${prefix}-${shot}.png`;
    });
    const stageImg = document.getElementById("workbench-shot");
    if (stageImg && workbenchViews[currentWorkbench]) {
      stageImg.src = `./assets/${prefix}-${workbenchViews[currentWorkbench].file}.png`;
    }
    const phoneImg = document.getElementById("phone-shot");
    if (phoneImg && phoneViews[currentPhone]) {
      phoneImg.src = `./assets/${prefix}-${currentPhone}.png`;
    }
  }

  function applyAppearance(announce = false) {
    root.setAttribute("data-theme", currentTheme);
    root.setAttribute("data-accent", currentAccent);

    const themeLabel = document.getElementById("theme-label");
    if (themeLabel) {
      themeLabel.textContent = currentTheme === "dark" ? "深色" : "浅色";
    }

    document.querySelectorAll("[data-set-accent]").forEach((btn) => {
      const active = btn.getAttribute("data-set-accent") === currentAccent;
      btn.setAttribute("aria-pressed", String(active));
    });

    syncThemeImages();

    if (announce) {
      const names = { default: "默认暖调", seablue: "海蓝", teal: "青绿", violet: "紫罗兰" };
      showToast(`外观已切换为${currentTheme === "dark" ? "深色" : "浅色"} · ${names[currentAccent] || "默认"}`);
    }
  }

  function selectRibbonTopic(id) {
    const topic = ribbonTopics.find((item) => item.id === id) || ribbonTopics[0];
    document.querySelectorAll("[data-topic-id]").forEach((el) => {
      el.setAttribute("aria-selected", String(el.getAttribute("data-topic-id") === topic.id));
    });

    const periodEl = document.getElementById("ins-period");
    const catEl = document.getElementById("ins-cat");
    const titleEl = document.getElementById("ins-title");
    const countEl = document.getElementById("ins-count");
    const summaryEl = document.getElementById("ins-summary");
    const ruleEl = document.getElementById("ins-rule");
    const evList = document.getElementById("ins-evidence");

    if (periodEl) periodEl.textContent = topic.period;
    if (catEl) {
      catEl.innerHTML = `<span class="dot" style="background:${topic.catColor}"></span>${topic.category}`;
    }
    if (titleEl) titleEl.textContent = topic.title;
    if (countEl) countEl.textContent = topic.charCount;
    if (summaryEl) summaryEl.textContent = topic.summary;
    if (ruleEl) ruleEl.textContent = topic.ruleNote;

    if (evList) {
      evList.innerHTML = topic.evidence
        .map(
          (ev) => `
          <div class="evidence-item">
            <div class="ev-top">
              <strong>${ev.source}</strong>
              <span class="tnum">${ev.time}</span>
            </div>
            <div class="ev-text">${ev.text}</div>
          </div>`
        )
        .join("");
    }
  }

  function selectWorkbench(key) {
    if (!workbenchViews[key]) return;
    currentWorkbench = key;
    document.querySelectorAll("[data-wb-tab]").forEach((btn) => {
      btn.setAttribute("aria-selected", String(btn.getAttribute("data-wb-tab") === key));
    });
    const info = workbenchViews[key];
    const titleEl = document.getElementById("wb-caption-title");
    const descEl = document.getElementById("wb-caption-desc");
    if (titleEl) titleEl.textContent = info.title;
    if (descEl) descEl.textContent = info.desc;
    syncThemeImages();
  }

  function selectPhone(key) {
    if (!phoneViews[key]) return;
    currentPhone = key;
    document.querySelectorAll("[data-phone-tab]").forEach((btn) => {
      btn.setAttribute("aria-selected", String(btn.getAttribute("data-phone-tab") === key));
    });
    const cap = document.getElementById("phone-caption");
    if (cap) cap.textContent = phoneViews[key];
    syncThemeImages();
  }

  document.addEventListener("DOMContentLoaded", () => {
    applyAppearance(false);
    selectRibbonTopic("t1");
    selectWorkbench("dashboard");
    selectPhone("dashboard-phone");

    // Theme toggle
    const themeBtn = document.getElementById("theme-toggle");
    if (themeBtn) {
      themeBtn.addEventListener("click", () => {
        currentTheme = currentTheme === "dark" ? "light" : "dark";
        localStorage.setItem(STORAGE_THEME, currentTheme);
        applyAppearance(true);
      });
    }

    // Accent buttons (both in nav and design section)
    document.querySelectorAll("[data-set-accent]").forEach((btn) => {
      btn.addEventListener("click", () => {
        currentAccent = btn.getAttribute("data-set-accent") || "default";
        localStorage.setItem(STORAGE_ACCENT, currentAccent);
        applyAppearance(true);
      });
    });

    // Day Ribbon hover & click
    document.querySelectorAll("[data-topic-id]").forEach((el) => {
      const activate = () => selectRibbonTopic(el.getAttribute("data-topic-id"));
      el.addEventListener("click", activate);
      el.addEventListener("mouseenter", activate);
    });

    // Workbench tabs
    document.querySelectorAll("[data-wb-tab]").forEach((btn) => {
      btn.addEventListener("click", () => selectWorkbench(btn.getAttribute("data-wb-tab")));
    });

    // Phone tabs
    document.querySelectorAll("[data-phone-tab]").forEach((btn) => {
      btn.addEventListener("click", () => selectPhone(btn.getAttribute("data-phone-tab")));
    });

    // Ecosystem connector filter
    document.querySelectorAll("[data-eco-filter]").forEach((btn) => {
      btn.addEventListener("click", () => {
        const group = btn.getAttribute("data-eco-filter");
        document.querySelectorAll("[data-eco-filter]").forEach((b) => {
          b.setAttribute("aria-pressed", String(b === btn));
        });
        document.querySelectorAll(".connector-card").forEach((card) => {
          const match = group === "all" || card.getAttribute("data-group") === group;
          card.hidden = !match;
        });
      });
    });

    // Quickstart OS / scenario tabs
    document.querySelectorAll("[data-qs-tab]").forEach((btn) => {
      btn.addEventListener("click", () => {
        const target = btn.getAttribute("data-qs-tab");
        document.querySelectorAll("[data-qs-tab]").forEach((b) => {
          b.setAttribute("aria-selected", String(b === btn));
        });
        document.querySelectorAll("[data-qs-panel]").forEach((panel) => {
          panel.hidden = panel.getAttribute("data-qs-panel") !== target;
        });
      });
    });

    // Copy buttons
    document.querySelectorAll("[data-copy]").forEach((btn) => {
      btn.addEventListener("click", async () => {
        const pre = btn.closest(".code-card")?.querySelector("pre");
        if (!pre) return;
        try {
          await navigator.clipboard.writeText(pre.innerText.trim());
          showToast("命令已复制到剪贴板");
        } catch {
          showToast("请选中代码块手动复制");
        }
      });
    });

    // Lightbox zoom for screenshots
    const lightbox = document.getElementById("lightbox");
    const lightboxImg = document.getElementById("lightbox-img");
    if (lightbox && lightboxImg) {
      document.querySelectorAll("[data-zoomable]").forEach((img) => {
        img.addEventListener("click", () => {
          lightboxImg.src = img.src;
          lightboxImg.alt = img.alt;
          lightbox.classList.add("open");
        });
      });
      lightbox.addEventListener("click", () => lightbox.classList.remove("open"));
      document.addEventListener("keydown", (e) => {
        if (e.key === "Escape") lightbox.classList.remove("open");
      });
    }
  });
})();
