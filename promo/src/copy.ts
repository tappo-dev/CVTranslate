export type BilingualLine = { chinese: string; english: string };

export const subtitles = {
  intro: {
    chinese: "好用的翻译工具已经很多，它只解决一件更小的事",
    english: "There are plenty of good translators. This one solves a smaller problem.",
  },
  problem: {
    chinese: "不离开当前输入位置，把刚写好的一段话马上译出来",
    english: "Translate what you just wrote, without leaving where you're typing",
  },
  triplePress: {
    chinese: "用中文写完，选中后连按三次 Ctrl+C，英文直接替换中文",
    english: "Write it in Chinese, select, press Ctrl+C three times, and English replaces it",
  },
  quadruplePress: {
    chinese: "想先看一眼？连按四次，在工作台确认后再复制",
    english: "Want to check first? Press four times, review in the workbench, then copy",
  },
  providers: {
    chinese: "接入自己的大模型，用人设让译文更接近你平时的语气",
    english: "Bring your own model and give it a persona to sound more like you",
  },
  privacy: {
    chinese: "设置和历史留在本机，没有账号，没有遥测",
    english: "Settings and history stay local. No account, no telemetry.",
  },
  outro: { chinese: "CVTranslate，现在就试试", english: "Try CVTranslate today" },
} satisfies Record<string, BilingualLine>;

export const repositoryUrl = "github.com/tappo-dev/CVTranslate";
