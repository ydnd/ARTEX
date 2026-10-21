import packageJson from "../../package.json";

const currentYear = new Date().getFullYear();

export const APP_CONFIG = {
  name: "Jiana",
  version: packageJson.version,
  copyright: `© ${currentYear}, ARTEX contributors and Jiana contributors.`,
  meta: {
    title: "Jiana — 安全研究控制台",
    description: "基于 ARTEX 的安全研究控制台，包含独立的安全加固与隐私修复。",
  },
};
