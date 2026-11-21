/** @type {import('@commitlint/types').UserConfig} */
export default {
  extends: ["@commitlint/config-conventional"],
  rules: {
    "type-enum": [
      2,
      "always",
      ["feat", "fix", "refactor", "perf", "test", "docs", "build", "ci", "chore", "revert"],
    ],
    "scope-enum": [
      2,
      "always",
      [
        "http", "service", "repo", "domain", "cache", "kafka", "webhook", "config", "obs", "migration",
        "web", "infra", "deploy", "loadtest", "deps", "ci", "docs", "agents",
      ],
    ],
    "scope-empty": [1, "never"],
    "subject-case": [2, "always", "lower-case"],
    "subject-full-stop": [2, "never", "."],
    "header-max-length": [2, "always", 100],
    "body-max-line-length": [1, "always", 100],
  },
};
