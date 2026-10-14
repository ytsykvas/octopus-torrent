# OctopusTorrent

Особистий локальний BitTorrent-клієнт для macOS (власний Apple Silicon Mac): зручне додавання `.torrent` і magnet, вибір файлів і папок, пауза й роздача, надійне відновлення після перезапуску. Без акаунтів, оплат, хмари, аналітики й вбудованого ШІ. **AI-first:** код і документація пишуться так, щоб агент-розробник продовжив роботу лише за цим репозиторієм.

## Поточний стан

**Етап P1 розпочато.** Є специфікації, макет, план, реєстр рішень і **перша мінімальна `.app`** (задача 003): вікно Wails з React-кнопкою, що викликає Go-метод. Продуктових функцій (торренти, бібліотека) ще немає.

| Що | Стан |
|---|---|
| Задачі [001](tasks/001-scope-and-decision-register.md), [002](tasks/002-repository-and-agent-entrypoint.md), [003](tasks/003-wails-macos-build-spike.md) | див. [навігацію](tasks/README.md#навігація-всіх-задач) |
| **Наступні задачі** | [004 — єдина команда перевірок](tasks/004-reproducible-check-commands.md), [005 — DTO і bridge-контракт](tasks/005-domain-dto-and-bridge-contract.md) |
| Покриття власного коду | 100% Go (оператори, сирі лічильники) і 100% TS (4 метрики на файл) — [звіт 003](docs/verification/003-wails-macos-build-spike.md) |

## Стек

React 19 + TypeScript 7 (strict) і Vite 8 · Wails v2.16.0 (системний WebView) · Go 1.27.2 · [anacrolix/torrent](https://github.com/anacrolix/torrent) (версію фіксує 007) · SQLite через repository (драйвер — 008). Точні версії, **політика оновлення** (остання стабільна версія не молодша за 14 днів; patch-релізи Go/Node — одразу) і причини — [toolchain-versions](docs/decisions/toolchain-versions.md); ціль macOS і bundle ID — [macos-target](docs/decisions/macos-target.md).

## Документи

| Документ | Для чого |
|---|---|
| [AGENTS.md](AGENTS.md) | **Почати тут, якщо ви агент:** обов’язкові правила сесії й межі модулів. |
| [PROJECT_REQUIREMENTS.md](PROJECT_REQUIREMENTS.md) | Поведінка й критерії: 80 ID вимог, 19 наскрізних сценаріїв. Головне джерело правди. |
| [PRODUCT_CONCEPT.md](PRODUCT_CONCEPT.md) | Ідея, особисті межі, порядок можливостей. |
| [APPLICATION_ARCHITECTURE.md](APPLICATION_ARCHITECTURE.md) | Компоненти, DTO, потоки, SQLite, видалення, macOS. |
| [ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md](ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md) | Технічний план рушія й Wails; припущення, які ще доводять прототипи. |
| [MOCKUP.html](MOCKUP.html) | Інтерактивний макет — орієнтир вигляду; демо-перемикачі й демо-логіка не є продуктом ([розбіжності](docs/decisions/project-baseline.md#макет-проти-специфікацій)). |
| [tasks/README.md](tasks/README.md) | План: 68 задач, залежності, матриці вимог і сценаріїв, правила виконання. |
| [docs/README.md](docs/README.md) | Індекс інженерної політики, рішень, доказів і документації фіч. |
| [docs/engineering/TDD_AND_QUALITY.md](docs/engineering/TDD_AND_QUALITY.md) | TDD, 100% покриття, відсутність боргу — обов’язково для кожної зміни. |
| [docs/decisions/project-baseline.md](docs/decisions/project-baseline.md) | Погоджений обсяг, інвентар вимог, розбіжності, середовище, реєстр відкритих рішень O1–O18. |
| [archive/](archive/LIBTORRENT_TECHNICAL_RESEARCH.md) | Історія відхиленого стеку (libtorrent). Лише для читання, не є вимогою. |

## Карта каталогів

Створені зараз:

```text
README.md, AGENTS.md          точка входу й правила агента
CLAUDE.md                     вхід для Claude Code: імпортує AGENTS.md, команди, загальна картина
PROJECT_REQUIREMENTS.md, PRODUCT_CONCEPT.md, APPLICATION_ARCHITECTURE.md,
ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md, MOCKUP.html   специфікації й макет
main.go, main_test.go         точка входу Wails: вбудований frontend/dist, run(start, stderr)
go.mod, go.sum                Go 1.27.2, Wails v2.16.0 (бібліотека + CLI як go tool)
wails.json                    конфігурація Wails (npm ci, npm run build)
internal/app/                 Wails facade (AppAPI): GetRuntimeInfo
frontend/src/bridge/          власна обгортка згенерованих bindings
frontend/src/features/        UI за функціями (зараз runtime-check)
frontend/src/test/            налаштування тестів
frontend/wailsjs/             bindings, згенеровані Wails — не редагувати вручну
frontend/package.json, package-lock.json, vite.config.ts, tsconfig.json
build/darwin/, build/appicon.png   Info.plist (bundle ID, мінімальна macOS), іконка
tasks/                        план і 68 numbered tasks
docs/                         політика, рішення (ADR), докази, фічі
archive/                      історичні матеріали, лише читання
```

**Ще не створено** — з [архітектури §3](APPLICATION_ARCHITECTURE.md#3-структура-репозиторію-для-ai-first): `internal/torrentengine/`, `internal/library/`, `internal/files/`, `internal/automation/`, `internal/platform/`, `internal/diagnostics/`, `frontend/src/{state,i18n,styles}/`, `scripts/`.

Простежити сценарій від UI до Go: [desktop-shell-build](docs/features/desktop-shell-build.md) — кнопка в `frontend/src/features/runtime-check` → `frontend/src/bridge/runtimeInfo.ts` → `frontend/wailsjs/go/app/App.js` → `internal/app.App.GetRuntimeInfo`. Для майбутньої вимоги: [TR-01](PROJECT_REQUIREMENTS.md#22-завантаження-й-роздача) → [матриця](tasks/README.md#повне-покриття-80-вимог) → [025](tasks/025-pause-resume-application.md) → `internal/app` → `internal/torrentengine` / `internal/library`.

## Команди збірки й перевірок

Перевірено в задачі 003 на Apple M4 / macOS 27.0.1 ([звіт](docs/verification/003-wails-macos-build-spike.md)). Усі команди виконуються з кореня репозиторію й не залежать від абсолютного шляху. Єдину команду для всіх перевірок створить [004](tasks/004-reproducible-check-commands.md).

**Передумови розробки** (готовій `.app` вони не потрібні): Xcode Command Line Tools (`xcode-select --install`), Go ≥ 1.27 (`brew install go`; точну 1.27.2 підтягне `go.mod`), Node.js ≥ 22.12 з npm ≥ 11.20 (перевірено 26.11.1 / 11.20.0; `frontend/.npmrc` задає `min-release-age=14`, тож npm не встановить версію, молодшу за 14 днів). Глобальний Wails CLI **не потрібен**: `go tool wails` бере версію з `go.mod`.

```sh
go tool wails doctor                          # перевірка середовища

# Frontend: залежності, типи, тести з порогом 100% на кожен файл, збірка
npm --prefix frontend ci
npm --prefix frontend run typecheck
npm --prefix frontend run test:coverage
npm --prefix frontend run build               # потрібно до go test: main вбудовує frontend/dist

# Go: формат, аналізатор, тести
gofmt -l .                                    # має бути порожньо
go vet ./...
go test ./...

# Bindings після зміни Go-facade (результат комітиться)
go tool wails generate module

# Релізна збірка → build/bin/OctopusTorrent.app (arm64, ad-hoc підпис)
go tool wails build -clean -trimpath

# Розробка з гарячим перезавантаженням frontend
go tool wails dev
```

**100% покриття Go** вимірюється разом із реальним запуском `.app`, бо `main()` виконується лише у справжньому застосунку:

```sh
C="$PWD/build/coverage"; rm -rf "$C"; mkdir -p "$C/go-unit" "$C/go-app"
go test -cover -coverpkg=./... ./... -args -test.gocoverdir="$C/go-unit"
GOFLAGS="-cover -coverpkg=./..." go tool wails build -clean -trimpath
open -n --env GOCOVERDIR="$C/go-app" build/bin/OctopusTorrent.app
# натиснути «Check Go connection», потім закрити застосунок (⌘Q)
go tool covdata textfmt -i="$C/go-unit,$C/go-app" -o "$C/go.out"
awk 'NR>1 && $NF==0' "$C/go.out"              # має бути порожньо: жодного непокритого блоку
go tool wails build -clean -trimpath          # повернути звичайну релізну збірку
```

Правила для майбутніх скриптів (AI-07): запуск із кореня, шляхи відносно кореня, без прихованих кроків; версії — у `go.mod`/`go.sum` і `frontend/package-lock.json`; згенеровані bindings відтворюються командою, а не редагуються.

## Робота з репозиторієм

Кожна задача — через RED → GREEN → REFACTOR, зі 100% покриттям власного виконуваного коду, актуальною документацією фічі й без нового боргу. Порядок дій — [tasks/README.md](tasks/README.md#як-виконати-одну-задачу), правила агента — [AGENTS.md](AGENTS.md). Особисті торренти, завантажені дані й робочі бази не комітяться (див. [.gitignore](.gitignore)); штучні тестові дані живуть у `testdata/`.
