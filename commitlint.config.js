// Conventional Commits — types/scopes per docs/09-development.md §9.
module.exports = {
  extends: ['@commitlint/config-conventional'],
  rules: {
    'type-enum': [
      2,
      'always',
      ['feat', 'fix', 'docs', 'test', 'refactor', 'perf', 'chore', 'build', 'ci'],
    ],
    'scope-enum': [2, 'always', ['api', 'web', 'db', 'infra', 'docs', 'chat', 'voice', 'i18n']],
  },
};
