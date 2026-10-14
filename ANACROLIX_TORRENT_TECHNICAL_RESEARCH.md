# OctopusTorrent — технічний план anacrolix/torrent і Wails

**Статус:** обраний стек, 14 жовтня 2026 року. React + TypeScript / Wails v2 / Go / anacrolix/torrent / SQLite. Це перевірка документації та план інтеграції; реальне передавання й відновлення нашим застосунком ще не прототиповані.

Поведінку продукту визначає `PROJECT_REQUIREMENTS.md`, межі модулів — `APPLICATION_ARCHITECTURE.md`. API нижче звірено з офіційними сторінками та відкритими вихідними файлами. Сторінки package documentation і master можуть описувати різні версії: перед кодом обираємо один module tag і повторно перевіряємо сигнатури на ньому. Старе дослідження libtorrent збережено в `archive/LIBTORRENT_TECHNICAL_RESEARCH.md` як довідка про попередній стек.

## 1. Що дає рушій

anacrolix/torrent — Go-бібліотека BitTorrent із metainfo/magnet, DHT, PEX, uTP, web seeds і v2. Вона використовується як компонент інших проєктів. Модель бібліотеки зручна для нашого Go-сервісу, але сама не зберігає продуктові колекції, правила й ручні рішення власника. [Офіційний репозиторій](https://github.com/anacrolix/torrent).

Для продукту потрібен власний adapter, який переводить наші UUID і команди у бібліотечні операції. Версія module фіксується в `go.mod`/`go.sum`; сторонній код не копіюється у наш сервіс і не переробляється для відповідності UI.

## 2. Карта API

| Потреба | API-кандидат |
|---|---|
| Client | `NewClient`, `Client.Close` |
| Імпорт | `AddTorrentSpec`, `TorrentSpecFromMagnetUri`, `TorrentSpecFromMetaInfoErr` |
| Metadata | `GotInfo`, `Info`, `Metainfo`, `Files` |
| Payload-пауза | `DisallowDataDownload`, `DisallowDataUpload` |
| Відновлення дозволів | `AllowDataDownload`, `AllowDataUpload` |
| Вибір файла | `File.SetPriority`, `PiecePriorityNone`, попередній пріоритет |
| Прогрес | `File.BytesCompleted`, `File.Length`, `Stats` |
| Перевірка | `VerifyDataContext` |
| Вилучення із client | `Drop`, `Closed` |

Новий TorrentSpec може задати початкову заборону download/upload. До отримання metadata `Info` може бути nil; це не готовий список файлів. `MergeSpec` не є способом замінити storage. [Документація API](https://pkg.go.dev/github.com/anacrolix/torrent).

Це карта інтеграції, а не обіцянка, що один виклик реалізує весь продуктовий сценарій. Adapter контролює порядок, локальний запис, cancellation, помилки та результат.

## 3. Manual pause і глобальна віддача

Зберігаємо окремо `manualPaused`, `globalUploadDisabled`, file wanted і фактичну помилку. Політика обчислює дозволи заново після кожної команди; натискання play не повертає upload, якщо його глобально вимкнено.

У поточному коді рушія download й upload мають окремі прапорці; download-заборона скасовує запити. В upload-реалізації є TODO про оновлення активних peer connections, тому лише наявність методу не доводить миттєве припинення всього буферизованого payload. Контрольований тест має визначити межу підтвердження й виміряти залишковий трафік. [Реалізація torrent.go](https://raw.githubusercontent.com/anacrolix/torrent/master/torrent.go).

Проєктне рішення: використовувати per-torrent дозволи для runtime-перемикача; нові записи додаються з відповідними початковими заборонами. Не мутувати конфігурацію client без перевіреного контракту потокобезпечності. Окремий `NoUpload` є конфігураційним вимкненням відправлення chunks. `Seed` керує готовністю роздавати без взаємної вигоди, а upload limiter стосується chunks. [Client config](https://raw.githubusercontent.com/anacrolix/torrent/master/config.go).

Для звичайної роздачі після завершення потрібна явно обрана seed-політика client і per-torrent дозволи. Зупинений запис не закриваємо тільки заради паузи. Протокольні повідомлення, tracker/DHT або перевірка диска не повинні видаватися за payload-завантаження.

## 4. Файли, папки й pieces

`File.SetPriority` змінює пріоритет pieces файла; `Download` використовує normal priority, а старий `Cancel` позначений deprecated. Readers теж впливають на piece-потреби, тому для простого клієнта не додаємо приховані readers до пропущених файлів. [File implementation](https://raw.githubusercontent.com/anacrolix/torrent/master/file.go).

Wanted належить нашій БД. Pause файла задає None, play відновлює збережений пріоритет. Кнопка папки проходить усі перевірені leaf indices; це операція сервісу, не об’єкт folder у engine. Згортання не має побічного ефекту. За partial wanted кнопка play вибирає всі нащадки.

Вибір завантаження не є дозволом upload конкретного файла й не видаляє bytes. Для v1 boundary pieces між двома файлами перевіряються окремо; при None одного файла не можна обіцяти відсутність будь-яких байтів його діапазону, якщо вони потрібні сусідньому. Завершені файли мають точний прогрес незалежно від wanted.

## 5. Metadata, шляхи й імпорт

`.torrent` розбираємо через metainfo до активного запису. Metainfo API має Load/UnmarshalInfo, v1/v2-відомості, file tree і piece layers. Повний metainfo зберігається без повторного перекодування info лише заради назви; ідентичність не підмінюється коротким display name. [Metainfo API](https://pkg.go.dev/github.com/anacrolix/torrent/metainfo).

Власна валідація задає межі розміру, числа файлів і глибини, перевіряє абсолютні шляхи, `..`, symlink та колізії. Назви — недовірені дані. Frontend preview не замінює Go-валідацію. Зберігаємо UUID та окремі повні v1/v2 ідентичності; hybrid або metadata, отримані пізніше, можуть виявити дублікат.

Для magnet pending import має cancellable очікування, власне тимчасове сховище й payload-заборони. Успішний confirm обирає остаточний storage; якщо потрібно Drop/re-add із отриманим metainfo, це робиться до дозволу payload зі збереженням UUID. Cancel очищає лише артефакти цього pendingImportID. Не обіцяємо відсутності порожніх файлів до підтвердження без тесту.

Finder, URL, кнопка й watch folder викликають один coordinator. Автоматичне додавання явно вмикається й створює manualPaused. Недоступна папка або правило не підставляють випадкове призначення.

## 6. Storage та відновлення

Storage API має файлові backend-и, options для шляхів і piece-completion реалізації, зокрема SQLite. Деякі custom-path конструктори deprecated; конкретний backend і його native залежності обираються на зафіксованому tag. Окремі backend-и використовують різну структуру папок, тому бібліотечний default не дорівнює нашому save path. [Storage API](https://pkg.go.dev/github.com/anacrolix/torrent/storage).

Проєктний план: власна SQLite-бібліотека з міграціями, payload у вибраній папці, окремий довговічний piece completion. На запуску додаємо записи з payload-забороною, відновлюємо wanted/паузу й лише потім дозволяємо transfer. Наявні bytes і completion-позначки перевіряються за обраною політикою; повна повторна перевірка може бути потрібною після пошкодження чи зовнішньої зміни.

Не переносимо до цього стеку обіцянку готового serialized fast-resume з іншої бібліотеки. Поведінку при звичайному close, crash, зіпсованому completion, переміщенні файлів і зміні storage треба довести окремо. Періодичне збереження продуктових налаштувань не відкладається до quit.

## 7. Прогрес, швидкості й помилки

Наш adapter агрегує завершені байти тільки wanted real files для прогресу торрента; folder progress охоплює реальні нащадки й показує wanted count окремо. Pad-файли не потрапляють у продуктовий вибір. Якщо wanted files відсутні, показуємо «Не вибрано файлів»; нульовий реальний файл не спричиняє ділення на нуль.

BytesCompleted є оцінкою наявних завершених/частково отриманих даних, а не лічильником швидкості. Для rates беремо дельту payload counters за час; після restart/reset потрібна нова базова вибірка. Перевірка даних може змінити завершені bytes. [File progress source](https://raw.githubusercontent.com/anacrolix/torrent/master/file.go), [Stats API](https://pkg.go.dev/github.com/anacrolix/torrent).

UI приховує rates для pause/done. ETA обчислюється лише за коректної додатної швидкості. Документуємо, чи bytes означають уже записані або також очікувані chunks; дані інспектора й картки походять з одного snapshot.

Go перетворює disk/network/metadata errors у власні AppError. Callback помилки запису не означає автоматично «торрент видалено»; його життєвий цикл відокремлено від ручної паузи. Error listeners, pollers і piece subscriptions мають teardown. Весь бізнес-стан не віддається React як бібліотечний pointer або mutable engine object.

## 8. Drop та видалення файлів

Бібліотечне вилучення й продуктове видалення — різні операції. Спершу зупиняємо payload, вилучаємо engine object і завершуємо пов’язані задачі; далі прибираємо запис. Окреме підтвердження дозволяє Go FileAccessService працювати з перевіреним переліком фізичних шляхів.

Спільні файли, symlink, зовнішні зміни й сторонній вміст не повинні бути видалені через назву папки. Використання macOS Trash перевіряється окремо; частковий результат записується в operation journal. SQLite і filesystem не є однією транзакцією. Crash посеред видалення має показати зрозумілий стан, а не загубити доказ залишених файлів.

## 9. Wails і frontend

Wails v2 надає desktop bindings і runtime; його стабільну гілку обрано для першої версії. Frontend із Vite входить у `.app`, усі system operations лишаються у Go. Встановлення залежностей розробки описане офіційно; точні toolchain/native prerequisites записуємо після власної збірки. [Wails introduction](https://wails.io/docs/introduction/), [Installation](https://wails.io/docs/gettingstarted/installation/).

Початковий snapshot потрібен навіть за emit/listener моделі: Go може почати роботу до React mount. Власні revision/operationID запобігають відкату UI старою подією. Browser component tests використовують fake bridge; справжні діалоги, Finder/URL і завершення `.app` перевіряються в Wails на Mac. [Events](https://wails.io/docs/reference/runtime/events/), [Options](https://wails.io/docs/reference/options/).

React переносить розміщення й дерево макета. WebView accessibility, фокус після rerender, shortcut-и, теми та непрозорий fallback є частиною реальних критеріїв. Не пов’язуємо business logic із CSS або WebView-подіями доступності мережі.

## 10. План прототипу й контрольні сценарії

| Перевірка | Що довести |
|---|---|
| Формати | `.torrent`, magnet, v1/v2/hybrid, multi-file, private, pad, zero-length, помилкові метадані. |
| Імпорт | Metadata-only cancel, два одночасні джерела, пізній дублікат, остаточна папка й storage. |
| Передавання | Manual pause обох payload-напрямків, resume, глобальна віддача, новий запис під глобальною політикою, seed після completion. |
| Файли | None/normal, partial folder, collapsed descendants, shared piece, відсутній wanted. |
| Відновлення | Close/relaunch, crash, completion corruption, modified/missing payload, ручна пауза до transfer. |
| Файлові помилки | Диск заповнений, permission denied, symlink/collision, Trash failure, перерване bulk delete. |
| Desktop | Холодний Finder/magnet, повторне відкриття, local assets, системні діалоги, WebKit/VoiceOver, quit. |
| Конкурентність | Скасування metadata, stale events, goroutine/listener teardown, data races, повільна SQLite-операція. |

Прототип працює з контрольованими тестовими торрентами й записує виміряний результат. Наявність API не позначається як пройдений тест. Після нього фіксуємо SQLite driver, storage/transport choices, dependency versions і точні build/check-команди.

MPL-2.0 ліцензія anacrolix/torrent і ліцензії залежностей залишаються частиною dependency inventory; особистий проєкт не вводить користувацької системи ліцензування. [LICENSE](https://github.com/anacrolix/torrent/blob/master/LICENSE).
