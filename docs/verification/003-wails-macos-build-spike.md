# Звіт 003 — перша Wails `.app` на власному Mac

**Задача:** [003](../../tasks/003-wails-macos-build-spike.md)  
**Дата:** 14–15 жовтня 2026 року  
**Версія змін:** коміт задачі 003 (батьківський `248079e`)  
**Фіча:** [desktop-shell-build](../features/desktop-shell-build.md); **ADR:** [toolchain-versions](../decisions/toolchain-versions.md), [macos-target](../decisions/macos-target.md)

## Середовище

| Параметр | Значення |
|---|---|
| Mac | Apple M4, `arm64`, 16 GiB, GPU Apple M4 (Metal 4) |
| macOS | 27.0.1 (26A434) |
| Xcode | Command Line Tools 2418 (повний Xcode не встановлений і не потрібний) |
| Go | 1.27.2 (`brew install go`, 2026-10-14) |
| Wails | v2.16.0 через `go tool wails` |
| Node.js / npm | 26.11.1 / 11.20.0 (Homebrew; оновлено з 26.0.0 / 11.12.1 2026-10-15, тим самим `brew upgrade` оновлено залежний `bitwarden-cli`) |
| Робочий каталог | корінь репозиторію; усі команди нижче — відносно нього |

`go tool wails doctor` → `SUCCESS Your system is ready for Wails development!` (необов’язкові Xcode, upx, nsis — `Available`, не встановлені).

## Архітектурний план сесії

- **Фактичний стан до задачі:** лише документація; коду, `go.mod`, `frontend/` немає.
- **Межі:** facade `internal/app` (єдиний bind), власний bridge `frontend/src/bridge`, UI `frontend/src/features/runtime-check`, точка входу `main.go` — за [архітектурою §2–§3](../../APPLICATION_ARCHITECTURE.md#3-структура-репозиторію-для-ai-first). Архітектура не змінюється; фактичні шляхи збігаються з картою.
- **Повторне використання:** шаблон Wails `react-ts` згенеровано в тимчасовому каталозі лише як довідку для `Info.plist`, `wails.json` і іконки; його код (`Greet`, CSS, шрифти) не перенесено.
- **Тестопридатність:** `main()` без розгалужень; логіка в `run(start, stderr)` із явною залежністю; React-компонент отримує функцію завантаження пропсом.
- **Ризики, перевірені першими (крок 0, поза репозиторієм):** `go tool wails` без глобального CLI — `pass`; запис лічильників покриття при виході `.app` через Apple Event — `pass` (лічильники `covcounters.*` з’являються, `main.go` має лічильник 1); TypeScript 7.0.2 + Vite 8 + Vitest 5 — `pass`; клік у WKWebView через System Events — `pass` після дозволу Accessibility для терміналу (дав власник).

## RED — тести до реалізації

| Тест | Команда | Результат (очікуваний `fail`) |
|---|---|---|
| `internal/app/app_test.go` | `go test ./internal/...` | `undefined: New`, `undefined: RuntimeInfo` — build failed |
| `main_test.go` | `go test .` | `undefined: run` — build failed |
| 4 frontend-файли тестів | `npx vitest run` (у `frontend/`) | `Failed to resolve import "./runtimeInfo" / "./RuntimeCheck" / "./App" / "./main"`; Test Files 4 failed |

## GREEN — фактичні перевірки

| # | Перевірка | Команда | Результат |
|---|---|---|---|
| 1 | Go-тести | `go test ./...` | `pass` (2 пакети) |
| 2 | TS-тести й покриття | `npm --prefix frontend run test:coverage` | `pass`: 4 файли, 10 тестів; Statements 19/19, Branches 10/10, Functions 6/6, Lines 19/19; «4 files fully covered» |
| 3 | Поріг ловить неімпортований файл | тимчасовий `src/zzUncovered.ts` без тестів → `vitest run --coverage` | `pass` (очікуваний `fail` порогу): 4 × `ERROR: Coverage … does not meet global threshold (100%) for src/zzUncovered.ts`; файл видалено |
| 4 | Go 100% із сирими лічильниками | юніт `-test.gocoverdir` + інструментована `.app` (`GOFLAGS="-cover -coverpkg=./..." go tool wails build`), клік, вихід; `go tool covdata textfmt`; `awk 'NR>1 && $NF==0'` | `pass`: 6 блоків (`main.go` ×4, `internal/app/app.go` ×2), усі з лічильником 1; нульових — 0; `covdata percent`: `octopustorrent` 100.0%, `octopustorrent/internal/app` 100.0%. Лише юніт-тести: `main` 83.3% — непокритий тільки `main()` |
| 5 | Формат і аналізатори | `gofmt -l .`; `go vet ./...`; `npm --prefix frontend run typecheck` | `pass`: порожньо; чисто; `tsc --noEmit` без помилок |
| 6 | Bindings | `go tool wails generate module` | `pass`: `frontend/wailsjs/go/app/App.{js,d.ts}`, `models.ts`, `runtime/` |
| 7 | Релізна збірка | `go tool wails build -clean -trimpath` | `pass`, exit 0, ~3 с: `build/bin/OctopusTorrent.app` |
| 8 | Склад пакета | `find`, `file`, `lipo -archs`, `du -sh` | `Contents/{Info.plist, MacOS/OctopusTorrent, Resources/iconfile.icns, _CodeSignature/CodeResources}`; Mach-O 64-bit `arm64`; 8.4 MB |
| 9 | Динамічні залежності | `otool -L` | `pass`: лише `/usr/lib/{libresolv.9, libobjc.A, libSystem.B}` і `/System/Library/Frameworks/{Cocoa, AppKit, Foundation, CoreFoundation, CoreGraphics, WebKit, Security, UniformTypeIdentifiers (weak), UserNotifications (weak)}`; без `/opt/homebrew` |
| 10 | Підпис | `codesign -dv --verbose=2`; `codesign --verify --deep --strict`; `spctl -a -vv` | `Identifier=dev.tsykvas.octopustorrent`, `Signature=adhoc`, `TeamIdentifier=not set`; verify `pass`; `spctl`: `rejected` (очікувано без Developer ID/notarization) |
| 11 | Info.plist | `plutil -p …/Info.plist` | `CFBundleIdentifier dev.tsykvas.octopustorrent`, `LSMinimumSystemVersion 27.0`, `CFBundleShortVersionString 0.0.1` |
| 12 | Немає мережевих/зовнішніх ресурсів UI | `grep '<script\|<link' frontend/dist/index.html`; URL у `dist/assets/*.js` | `pass`: один `<script src="/assets/index-….js">` (локальний); URL у бандлі — XML namespace (`w3.org`) і текст повідомлень `react.dev/errors/`, не завантажувані ресурси |
| 13 | Абсолютні шляхи в бінарнику | `strings … \| grep -c /Users/` | без `-trimpath`: 142 → з `-trimpath`: **0**; `go version -m`: `-trimpath=true` |
| 14 | Режим розробки | `go tool wails dev -noreload` у фоні; клік; зупинка `SIGINT` | `pass`: Vite на `localhost:5174` (5173 зайнятий іншим проєктом власника), відповідь Go у вікні; після зупинки процесів `wails dev`/`vite` цього проєкту немає |
| 15 | Запуск `.app` (LaunchServices) + клік, двічі | `open build/bin/OctopusTorrent.app`; `osascript click.applescript OctopusTorrent "Check Go connection"`; `osascript … to quit` | `pass` ×2: текст вікна `Go go1.27.2 · darwin/arm64`; дочірніх процесів немає; процесів `go`/`node`/`wails` цього проєкту немає; вихід коректний |
| 16 | **Відкриття з Finder** | власник: подвійний клік у Finder, натискання кнопки; я — читання тексту вікна через Accessibility | `pass`: власник бачить `Go go1.27.2 · darwin/arm64`; процес `ppid=1 (/sbin/launchd)`; прочитаний текст вікна збігається; quit ok |
| 17 | **Без інтернету**, повторний запуск | `offline_check.sh` (мережу вимикає власник; скрипт чекає зникнення маршруту, двічі запускає `.app`, натискає кнопку, рахує сокети, закриває) | `not_run` — заплановано з власником |
| 18 | Збірка з чистої копії | експорт `git ls-files --cached --others --exclude-standard` у тимчасовий каталог → `go tool wails build -clean -trimpath` (сам виконує `npm ci` з `.npmrc`) | `pass`: «Installing frontend dependencies: Done», зібрано; `frontend/dist/assets/*.js` — однаковий SHA-256 (`9a430be283b183dd…`); бінарник без підпису — однаковий SHA-256 `2cd78877…4fc5e679` (відтворювана збірка) |
| 20 | Вік кожної заблокованої версії ≥ 14 днів | `check_release_age.py`: час публікації з registry.npmjs.org для всіх записів `package-lock.json` і з proxy.golang.org для `go list -m all` | `pass`: npm 159, Go 142 версій; молодших за межу 2026-09-30 немає; найновіші `chai@6.3.0` (2026-09-30), `wails/v2@v2.16.0` (2026-09-14) |
| 21 | Вразливості | `npm --prefix frontend audit`; `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | `pass`: `found 0 vulnerabilities`; `No vulnerabilities found` |
| 22 | npm install-скрипти | `npm install-scripts ls` | один пакет (`fsevents@2.3.3`) — заборонено через `allowScripts`; `npm ci` без попереджень; `require('fsevents')` завантажується (готовий бінарник) |
| 19 | Посилання документації | `check_links.py` для Markdown-файлів Git | див. «Підсумок» |

Скрипти сесії (`click.applescript`, `offline_check.sh`, `check_release_age.py`, перевірка посилань) лишилися в тимчасовому каталозі як одноразові інструменти ручної перевірки. Постійну автоматизовану перевірку з тестами створює [004](../../tasks/004-reproducible-check-commands.md).

## Перегляд версій 2026-10-15

Власник попросив тримати стек актуальним. Я спершу оновив frontend до найновіших patch-версій (react 19.3.1, vite 8.3.4 тощо, частина вийшла за 1–8 днів до того) і Node до 26.11.1. Власник зупинив відмову від 14-денної паузи як вразливість ланцюга постачання. Підсумок:

- frontend повернуто на версії, не молодші за 14 днів; правило поширено на транзитивні пакети через `frontend/.npmrc` `min-release-age=14` і lockfile перегенеровано;
- виявлено, що Go 1.27.2 у першому виборі мав 12 днів, тобто початкове правило було порушене; власник затвердив окреме правило для інструментів: patch-релізи Go/Node — одразу, нові лінії — через 14 днів (Go 1.27.2 і Node 26.11.1 лишаються);
- транзитивні Go-модулі — за Go MVS (як у Wails), вразливі — примусово; `govulncheck` і `npm audit` чисті;
- після змін повторено пп. 1–7, 9–13, 15, 18 — `pass`; Go-покриття знову 100% (0 з 6 блоків із нульовим лічильником).

Політика — [toolchain-versions](../decisions/toolchain-versions.md); автоматизацію перевірок віку/вразливостей/оновлень додано в обсяг [004](../../tasks/004-reproducible-check-commands.md).

## REFACTOR і перевірка боргу

- Код шаблону Wails не перенесено; `main` зведено до одного виклику, щоб інструментований запуск покривав його повністю.
- Знайдено під час перевірки й виправлено: абсолютні шляхи розробника в бінарнику (`-trimpath` у релізній команді); кеш Wails `frontend/package.json.md5` додано в `.gitignore`.
- Після змін повторено тести, покриття й збірку — зелені.
- TODO немає; тимчасових обходів немає. Прототипний UI — проміжний результат до 034/035/040, не борг.

## Обмеження перевірки сумісності

- Перевірено **лише** macOS 27.0.1 на Apple M4 (`arm64`). Інші версії macOS і Intel Mac не перевірялися й не заявляються (`LSMinimumSystemVersion 27.0`).
- Підпис ad-hoc; Gatekeeper відхиляє пакет при перевірці `spctl`. Відкривається, бо локально зібраний пакет не має карантину. Перенесення на інший Mac через завантаження не перевірялося (066).
- Попередження лінкера `built for newer 'macOS' version (13.0) than being linked (11.0)` — на запуск не впливає (див. ADR toolchain-versions).
- ESLint/аналізатори React і єдина команда «все перевірити» — задача 004.

## Підсумок

`not_run` для п. 17 (офлайн) — задача 003 залишається `in_progress` до виконання.
