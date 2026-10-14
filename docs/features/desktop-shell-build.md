# Desktop-оболонка, збірка `.app` і перший зв’язок React → Go

**Статус:** `implemented` (прототип задачі 003; продуктовий UI замінить його в 034/035/040)  
**Вимоги:** REL-01, REL-02, STACK-05, UI-04 (частково), AI-07, QA-01, QA-02, QA-05 (частково)  
**Пов’язані задачі:** [003](../../tasks/003-wails-macos-build-spike.md); наступні — [004](../../tasks/004-reproducible-check-commands.md) (єдина команда перевірок), [034](../../tasks/034-wails-facade-real-bindings.md) (справжній facade), [035](../../tasks/035-react-bridge-state-resync.md) (bridge і стан)  
**Остання перевірена версія:** коміт задачі 003

## Мета й поведінка

Власник відкриває `OctopusTorrent.app` з Finder на своєму Mac. У вікні Wails (системний WebView) показано локальний React UI з кнопкою **Check Go connection**. Натискання викликає Go-метод через згенеровані bindings і показує відповідь `Go go1.27.2 · darwin/arm64`. Помилка виклику показується як `role="alert"` з текстом причини. UI вбудований у бінарник і працює без інтернету; готовий пакет не потребує Go, Node.js чи Wails CLI.

Реалізовано лише цей доказ шляху. Список торрентів, теми, i18n та інше — у наступних задачах (тексти прототипу англійською, локалізацію додає 041).

## Архітектурний аналіз перед зміною

- Перший код репозиторію; повторно використовувати нічого. Межі взято з [архітектури §2–§3](../../APPLICATION_ARCHITECTURE.md#2-компоненти-й-напрямки-залежностей): facade у `internal/app`, власний bridge у `frontend/src/bridge`, UI у `frontend/src/features`.
- Стану й побічних ефектів у Go немає; `GetRuntimeInfo` — чиста функція від вбудованого runtime. Контекст Wails (`OnStartup`) не потрібен, тому не доданий.
- Точку входу `main` зведено до одного рядка без розгалужень; уся логіка в `run(start, stderr)` із явною залежністю `start` (у продукті — `wails.Run`), що робить її тестопридатною без вікна.
- React-компонент отримує `loadRuntimeInfo` пропсом; лише `App` під’єднує реальний bridge. Тести працюють із fake-функцією, без Wails.

## Контракти й дані

| Go (`internal/app`) | JSON / TS (`frontend/src/bridge/runtimeInfo.ts`) |
|---|---|
| `func (*App) GetRuntimeInfo() RuntimeInfo` | `getRuntimeInfo(): Promise<RuntimeInfo>` |
| `RuntimeInfo{GoVersion, OS, Arch string}` | `{ goVersion: string; os: string; arch: string }` |

Bridge повертає власний plain-об’єкт, а не згенерований клас `app.RuntimeInfo`. Відхилений проміс передається далі без змін; компонент перетворює `Error` на його `message`, інше значення — на `String(value)`.

## Сценарії й TDD

| Сценарій | Тест | RED (до реалізації) |
|---|---|---|
| Facade повертає значення вбудованого runtime; JSON-ключі контракту | `internal/app/app_test.go` | `undefined: New`, `undefined: RuntimeInfo` |
| `run` передає `wails.Run` назву, вбудований `frontend/dist/index.html`, рівно один bind `*app.App`; помилка старту → stderr і код 1 | `main_test.go` | `undefined: run` |
| Bridge: успіх → plain DTO; помилка → reject | `src/bridge/runtimeInfo.test.ts` | `Failed to resolve import "./runtimeInfo"` |
| UI: початковий стан, «Checking…», успіх, помилки `Error`/не-`Error`, клавіатура (Tab + Enter) і фокус | `src/features/runtime-check/RuntimeCheck.test.tsx` | `Failed to resolve import "./RuntimeCheck"` |
| `App` під’єднує bridge; заголовок | `src/App.test.tsx` | `Failed to resolve import "./App"` |
| `main.tsx` монтує застосунок у `#root`; без `#root` — зрозуміла помилка | `src/main.test.tsx` | `Failed to resolve import "./main"` |
| Реальна `.app`: клік → відповідь Go; запуск із Finder; повторний запуск; офлайн | ручна + System Events | див. [звіт 003](../verification/003-wails-macos-build-spike.md) |

## Реалізація й повторне використання

```text
main.go                         embed frontend/dist; run(start, stderr); main()
internal/app/app.go             App facade, RuntimeInfo DTO
frontend/src/main.tsx           монтування в #root
frontend/src/App.tsx            композиція: RuntimeCheck + реальний bridge
frontend/src/bridge/runtimeInfo.ts       єдиний виклик згенерованого GetRuntimeInfo
frontend/src/features/runtime-check/     UI перевірки зв’язку
frontend/wailsjs/               згенеровано `go tool wails generate module` — не редагувати
build/darwin/Info*.plist        bundle ID, мінімальна macOS (ADR macos-target)
wails.json                      npm ci, npm run build
```

## Перевірки та 100% покриття

Команди — у [README](../../README.md#команди-збірки-й-перевірок); фактичні результати — у [звіті 003](../verification/003-wails-macos-build-spike.md).

- **Go:** юніт-профіль (`-test.gocoverdir`) + профіль інструментованого реального запуску `.app` (`GOFLAGS=-cover`), злиті `go tool covdata textfmt`. Інвентар: `main.go`, `internal/app/app.go`. Блоків із нульовим лічильником — 0; `covdata percent`: 100,0% для обох пакетів. Одних юніт-тестів недостатньо: вони дають 83,3% для `main` (непокритий рядок `main()`), тому інструментований запуск є обов’язковим.
- **TypeScript:** Vitest + v8, `include: src/**/*.{ts,tsx}`, поріг 100 для lines/statements/functions/branches **на кожен файл**. 4 файли, 19/19 операторів, 10/10 гілок, 6/6 функцій. Негативна перевірка: тимчасовий неімпортований файл без тестів валить поріг.
- Не обліковуються як власний код: `frontend/wailsjs/` (відтворюваний згенерований), `src/**/*.test.*`, `src/test/setup.ts` (тестова інфраструктура).
- Аналізатори: `gofmt -l` порожньо, `go vet` чисто, `tsc --noEmit` (strict, `noUncheckedIndexedAccess`, `noUnused*`) чисто. ESLint і централізована команда — 004.

## Рефакторинг і перевірка боргу

- Логіку шаблону Wails (`Greet`, `OnStartup` без потреби, шрифти, лого, CSS) не перенесено.
- `main` спрощено до виклику без розгалужень, щоб інструментований запуск покривав його повністю.
- Знайдено й виправлено: абсолютні шляхи розробника в бінарнику (`-trimpath`); кеш Wails `frontend/package.json.md5` додано в `.gitignore`.
- Тимчасових обходів і TODO немає. Прототипний UI — погоджена проміжна фіча до 034/035/040, а не борг.

## Використання й відновлення

1. Встановити Go (`brew install go`), мати Node.js ≥ 22.12 і Xcode Command Line Tools.
2. `go tool wails build -clean -trimpath` → `build/bin/OctopusTorrent.app`.
3. Відкрити з Finder. Якщо macOS старіша за 27.0, запуск заборонено (див. ADR).

## Історія змін і докази

| Дата | Зміна | Докази |
|---|---|---|
| 2026-10-14 | Перша `.app`, facade `GetRuntimeInfo`, bridge, UI перевірки, 100% покриття Go/TS | [звіт 003](../verification/003-wails-macos-build-spike.md), ADR [toolchain-versions](../decisions/toolchain-versions.md), [macos-target](../decisions/macos-target.md) |
