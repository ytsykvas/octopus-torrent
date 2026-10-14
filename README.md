# OctopusTorrent

Особистий локальний BitTorrent-клієнт для macOS (власний Apple Silicon Mac): зручне додавання `.torrent` і magnet, вибір файлів і папок, пауза й роздача, надійне відновлення після перезапуску. Без акаунтів, оплат, хмари, аналітики й вбудованого ШІ. **AI-first:** код і документація пишуться так, щоб агент-розробник продовжив роботу лише за цим репозиторієм.

## Поточний стан

**Етап P0 завершено: є специфікації, інтерактивний HTML-макет, план задач і реєстр рішень. Коду застосунку ще немає** — жодного Go-, TypeScript- чи Wails-файла, жодної робочої команди збірки чи тестів.

| Що | Стан |
|---|---|
| Вимоги, концепт, архітектура, дослідження рушія | готові, див. нижче |
| Задачі [001](tasks/001-scope-and-decision-register.md), [002](tasks/002-repository-and-agent-entrypoint.md) | `done` |
| **Наступна задача** | [003 — перша Wails `.app` на власному Mac](tasks/003-wails-macos-build-spike.md) (потрібно встановити Go і Wails CLI — їх ще немає на Mac, див. [baseline §5](docs/decisions/project-baseline.md#5-середовище-розробки)) |
| Покриття коду | `not_applicable` — виконуваного коду немає |

Актуальні статуси всіх 68 задач — у [навігації плану](tasks/README.md#навігація-всіх-задач).

## Стек

React + TypeScript (strict) і Vite · Wails v2 (системний WebView) · Go · [anacrolix/torrent](https://github.com/anacrolix/torrent) · SQLite через repository. Точні версії ще не зафіксовані — це рішення O2 задач 003/004/007.

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
PROJECT_REQUIREMENTS.md, PRODUCT_CONCEPT.md, APPLICATION_ARCHITECTURE.md,
ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md, MOCKUP.html   специфікації й макет
tasks/                        план і 68 numbered tasks
docs/engineering/             інженерна політика
docs/decisions/               рішення (ADR) і шаблон _ADR_TEMPLATE.md
docs/verification/            докази виконання задач
docs/features/                документація фіч (поки лише шаблон)
archive/                      історичні матеріали, лише читання
```

**Ще не створено** — запланована структура з [архітектури §3](APPLICATION_ARCHITECTURE.md#3-структура-репозиторію-для-ai-first); фактичні шляхи підтвердить 003:

```text
main.go, wails.json, go.mod, go.sum    запуск, конфігурація Wails, Go-залежності
internal/app/                          Wails facade, lifecycle, координація команд
internal/torrentengine/                інтерфейс і адаптер anacrolix/torrent
internal/library/                      SQLite repositories і migrations
internal/files/                        шляхи, папки, безпечне видалення
internal/automation/                   правила й папка спостереження
internal/platform/                     Finder, URL, системні діалоги
internal/diagnostics/                  локальна діагностика
frontend/src/{features,bridge,state,i18n,styles}/   React UI
frontend/wailsjs/                      bindings, згенеровані Wails (не редагувати вручну)
scripts/                               відтворювані команди збірки й перевірок
```

Простежити один сценарій: вимога (наприклад, [TR-01 пауза](PROJECT_REQUIREMENTS.md#22-завантаження-й-роздача)) → основна задача в [матриці вимог](tasks/README.md#повне-покриття-80-вимог) ([025](tasks/025-pause-resume-application.md)) → майбутні модулі `internal/app` → `internal/torrentengine` / `internal/library` → `frontend/src/features`.

## Команди збірки й перевірок

**Жодна команда ще не працює і не перевірена.** Набір команд створюється й перевіряється в [003](tasks/003-wails-macos-build-spike.md) (перша збірка `.app`) і [004](tasks/004-reproducible-check-commands.md) (єдина локальна перевірка: тести, 100% покриття, аналізатори, bindings, документація). До цього не вважати робочими навіть стандартні `wails build`, `go test ./...` чи `npm test`.

Вимоги до майбутніх скриптів і команд (AI-07):

- Запускаються з кореня репозиторію й не залежать від абсолютного шляху розробника, домашнього каталогу чи прихованих кроків; шляхи — відносно кореня.
- Версії Go/Wails CLI/Node.js задокументовані тут, Go-залежності — в `go.mod`/`go.sum`, frontend — у lockfile.
- Native prerequisites Mac (Xcode Command Line Tools тощо) перелічені тут після перевірки в 003.
- Згенеровані bindings відтворюються командою й не редагуються вручну.

## Робота з репозиторієм

Кожна задача — через RED → GREEN → REFACTOR, зі 100% покриттям власного виконуваного коду, актуальною документацією фічі й без нового боргу. Порядок дій — [tasks/README.md](tasks/README.md#як-виконати-одну-задачу), правила агента — [AGENTS.md](AGENTS.md). Особисті торренти, завантажені дані й робочі бази не комітяться (див. [.gitignore](.gitignore)); штучні тестові дані живуть у `testdata/`.
