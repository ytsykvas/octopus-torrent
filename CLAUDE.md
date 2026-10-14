# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Обов’язкові правила агента — в AGENTS.md, вони діють повністю:

@AGENTS.md

Документація й спілкування з власником — українською; ідентифікатори коду й назви модулів — англійською.

## Команди

Усі команди — з кореня репозиторію. Повний перевірений перелік і передумови — [README → Команди](README.md#команди-збірки-й-перевірок). Єдиної команди «перевірити все» ще немає, її створює задача [004](tasks/004-reproducible-check-commands.md).

```sh
npm --prefix frontend ci                      # .npmrc: min-release-age=14 — не обходити
npm --prefix frontend run typecheck
npm --prefix frontend run test:coverage       # поріг 100% на кожен файл, 4 метрики
npm --prefix frontend run build               # обов’язково до go test: main.go вбудовує frontend/dist
gofmt -l . && go vet ./... && go test ./...
go tool wails generate module                 # після зміни методів internal/app; frontend/wailsjs комітиться
go tool wails build -clean -trimpath          # → build/bin/OctopusTorrent.app
go tool wails dev
```

Окремий тест:

```sh
go test ./internal/app -run TestGetRuntimeInfoReportsCompiledGoRuntime -v
npm --prefix frontend test -- src/features/runtime-check/RuntimeCheck.test.tsx -t 'Go'
```

Для одного файла використовуйте `test`, а не `test:coverage`: поріг `perFile` впаде на файлах, які цей запуск не виконав.

Wails CLI береться лише через `go tool wails` (версія закріплена в `go.mod` директивою `tool`); глобальний `wails` не встановлювати.

## Покриття Go

`main()` виконується лише в справжньому застосунку, тому 100% для Go — це злитий профіль юніт-тестів та інструментованої `.app` (процедура в [README](README.md#команди-збірки-й-перевірок)). Критерій — порожній вивід `awk 'NR>1 && $NF==0' build/coverage/go.out`, а не округлений напис `100.0%`. Після інструментованої збірки зробіть звичайну релізну.

Для TS джерела задає `vite.config.ts` (`include: src/**/*.{ts,tsx}`). `frontend/wailsjs/` лежить поза `src/` і не є власним кодом; власні обгортки над ним живуть у `frontend/src/bridge` і вимірюються.

## Загальна картина

- Зараз реалізована лише оболонка із задачі 003: `frontend/src/features/runtime-check` → `frontend/src/bridge/runtimeInfo.ts` → `frontend/wailsjs/go/app/App.js` → `internal/app.App.GetRuntimeInfo`. Цей самий ланцюжок — шаблон для кожної нової команди UI → Go.
- `main.go` тримає `main()` без розгалужень: `run(start, stderr)` приймає функцію запуску Wails, тож тести підставляють фейк замість вікна. Нові залежності facade передаються явно через `app.New(...)` і `options.App.Bind`.
- Компоненти React отримують залежності від bridge пропсами (див. `RuntimeCheck`), тому тестуються без Wails. `App.tsx` з’єднує їх зі справжнім bridge.
- Цільова структура (`internal/torrentengine`, `internal/library`, `internal/files`, `internal/platform`, `frontend/src/state` тощо) описана в [архітектурі §3](APPLICATION_ARCHITECTURE.md#3-структура-репозиторію-для-ai-first). Більшості каталогів ще немає. Їх створюють numbered tasks, а не вигадуються наперед.
- Джерело поведінки — [PROJECT_REQUIREMENTS.md](PROJECT_REQUIREMENTS.md) з ID вимог. Що робити далі — [tasks/README.md](tasks/README.md#навігація-всіх-задач). Відкриті питання O1–O18 — в [реєстрі рішень](docs/decisions/project-baseline.md#6-реєстр-відкритих-рішень). `MOCKUP.html` — лише орієнтир вигляду; його демо-логіка не є вимогою.

## Особливості середовища

- Перевірене середовище одне: Apple M4, macOS 27.0.1 arm64 ([macos-target](docs/decisions/macos-target.md)). Bundle ID — `dev.tsykvas.octopustorrent`; закрити застосунок зі скрипта: `osascript -e 'tell application id "dev.tsykvas.octopustorrent" to quit'` (повертає -128, але застосунок завершується).
- Попередження лінкера про macOS 13.0 проти 11.0 нешкідливе ([toolchain-versions](docs/decisions/toolchain-versions.md#наслідки)).
- Перевіряйте посилання в Markdown лише у файлах Git (`git ls-files --cached --others --exclude-standard '*.md'`), бо інакше перевірка заходить у `frontend/node_modules`.
