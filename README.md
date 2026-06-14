# Habr Telegram Digest Bot

Бот для Telegram, который собирает статьи с Хабра, отбирает полезные по выбранным хабам и присылает короткий дайджест.

Готовый бот: `@HabrParser777_bot`.

Основная идея простая: бот смотрит не последние 7 дней, а один конкретный день, который был 7 дней назад. Например, если сегодня 13 июня, бот берёт статьи только за 6 июня с 00:00 до 23:59. За неделю статья уже успевает набрать рейтинг и комментарии, а периоды рассылки не пересекаются.

По умолчанию summary делает локальная бесплатная модель через Ollama. Если модель недоступна, бот не падает и берёт короткий фрагмент из текста статьи.

## Что Умеет

- показывает список хабов Хабра;
- даёт выбрать хабы через inline-кнопки;
- хранит выбранные хабы и настройку авторассылки в SQLite;
- вручную отправляет дайджест командой `/latest`;
- каждый день отправляет автоматический дайджест тем, кто включил `/auto_on`;
- генерирует summary через локальную LLM;
- не сохраняет статьи и полный текст статей в базу.

## Команды

`/start` — краткое описание бота.

`/categories` — выбрать или убрать хабы.

`/my_categories` — посмотреть выбранные хабы.

`/latest` — получить статьи за день, который был 7 дней назад.

`/today` — получить статьи за текущий день.

`/auto_on` — включить ежедневную рассылку.

`/auto_off` — выключить ежедневную рассылку.

`/help` — список команд.

## Быстрый Запуск Через Docker

Создайте `.env`:

```bash
cp .env.example .env
```

Откройте `.env` и укажите токен Telegram-бота. Для текущего проекта используется бот `@HabrParser777_bot`:

```text
TELEGRAM_BOT_TOKEN=...
```

Для Docker значение `LLM_BASE_URL` должно быть таким:

```text
LLM_BASE_URL=http://ollama:11434
```

Запустите проект:

```bash
docker compose up -d --build
```

При первом запуске Docker скачает модель `qwen2.5:3b-instruct`. Это может занять несколько минут.

Проверить состояние контейнеров:

```bash
docker compose ps -a
```

Посмотреть логи бота:

```bash
docker compose logs -f habr-tg-bot
```

Остановить:

```bash
docker compose down
```

## Telegram Bot

В README используется готовый бот: `@HabrParser777_bot`.

Токен хранится только в локальном `.env`. Не коммитьте `.env` и не вставляйте токен в README.

## Локальный Запуск Без Docker

Установите Ollama и скачайте модель:

```bash
ollama pull qwen2.5:3b-instruct
```

В `.env` для локального запуска укажите:

```text
LLM_BASE_URL=http://localhost:11434
LLM_API_KEY=
LLM_MODEL=qwen2.5:3b-instruct
SQLITE_PATH=data/habr_tg_bot.sqlite
```

Запустите бота:

```bash
go mod download
go run ./cmd/habr-tg-bot
```

## Настройки

Основные переменные окружения:

```text
TELEGRAM_BOT_TOKEN=...
SQLITE_PATH=data/habr_tg_bot.sqlite
HABR_BASE_URL=https://habr.com
HTTP_TIMEOUT=20s
DAILY_DIGEST_TIME=09:00
TIMEZONE=Europe/Moscow
LLM_BASE_URL=http://ollama:11434
LLM_API_KEY=
LLM_MODEL=qwen2.5:3b-instruct
MIN_ARTICLE_SCORE=60
MAX_ARTICLES_PER_DIGEST=5
LOG_LEVEL=info
KNOWN_AUTHORS=
```

`LLM_API_KEY` для Ollama не нужен. Поле можно оставить пустым.

Если VPS слабый, можно поставить модель поменьше, например `qwen2.5:1.5b-instruct`. Тогда поменяйте `LLM_MODEL` в `.env` и перезапустите `ollama-pull` или скачайте модель вручную.

## Как Работает Отбор Статей

Статья проходит в дайджест, если её score не ниже `MIN_ARTICLE_SCORE`.

В score учитываются:

- совпадение с выбранными хабами;
- рейтинг статьи;
- количество комментариев;
- карма автора, если её удалось достать из публичной страницы;
- автор из списка `KNOWN_AUTHORS`;
- наличие кода;
- технические ключевые слова;
- production-контекст;
- рекламные и маркетинговые признаки;
- вакансии;
- слишком короткий или слишком общий текст;
- новости без технических деталей;
- чистые переводы без добавленной пользы.

Дефолтные веса:

```text
+30  совпадение с выбранной категорией
+20  высокий рейтинг статьи
+15  много комментариев
+15  высокая карма автора
+10  автор из KNOWN_AUTHORS
+10  наличие кода
+10  технические ключевые слова
+10  production-контекст
-30  рекламные признаки
-20  вакансия
-15  слишком короткий текст
-15  слишком общий текст
-15  новость без технических деталей
-15  чистый перевод без добавленной ценности
-30  маркетинговая статья
```

Веса можно менять через переменные `SCORE_*` в `.env`.

## Откуда Берутся Статьи И Хабы

RSS Хабра не используется как основной источник. Он удобен для свежей ленты, но не гарантирует полный набор статей за день, который был 7 дней назад.

Бот читает публичные страницы:

```text
https://habr.com/ru/articles/pageN/
https://habr.com/en/articles/pageN/
https://habr.com/ru/hubs/
https://habr.com/en/hubs/
```

Парсер лежит в `internal/habr`. Если Хабр поменяет разметку, править нужно в основном этот пакет.

Хабы не захардкожены. Бот получает их со страниц Хабра и сохраняет выбранные пользователем alias-ы в SQLite.

## Что Хранится В SQLite

Создаются две основные таблицы:

```text
users
user_categories
```

Хранится только:

- Telegram user id;
- включена ли авторассылка;
- выбранные категории;
- даты создания и обновления настроек.

Статьи, тексты статей и summary в базу не пишутся.

## Почему Нет Истории Отправок

Для ежедневной рассылки история не нужна: каждый запуск берёт новый календарный день, который не пересекается с предыдущими.

Ограничение есть у ручной команды `/latest`: если нажать её несколько раз в один день, бот может прислать те же статьи ещё раз. Это сделано намеренно, чтобы не усложнять базу.

Если нужно убрать дубли и для ручного режима, можно добавить таблицу:

```sql
CREATE TABLE IF NOT EXISTS sent_articles (
    telegram_user_id INTEGER NOT NULL,
    article_url TEXT NOT NULL,
    sent_at TEXT NOT NULL,
    PRIMARY KEY (telegram_user_id, article_url)
);
```

Сейчас приложение эту таблицу не создаёт и не использует.

## Формат Сообщения

Telegram parse mode: `HTML`.

```html
<a href="https://habr.com/ru/articles/123456/">Название статьи</a>

статья объясняет проблему, показывает подход к решению и кому это может быть полезно.

#go #backend #postgresql
```

Заголовок, ссылка, summary и хештеги экранируются перед отправкой.

## Структура Проекта

```text
cmd/habr-tg-bot/main.go
internal/bot
internal/config
internal/domain
internal/habr
internal/hashtags
internal/logging
internal/scheduler
internal/scoring
internal/service
internal/storage
internal/summary
migrations/001_init.sql
Dockerfile
docker-compose.yml
```

Главные места:

- `internal/habr` — HTTP-клиент и парсер Хабра;
- `internal/service` — сборка дайджеста;
- `internal/scoring` — score-модель;
- `internal/summary` — LLM и fallback summary;
- `internal/bot` — команды Telegram и inline-кнопки;
- `internal/scheduler` — ежедневная рассылка;
- `internal/storage` — SQLite.

## Обновление На VPS

```bash
git pull
docker compose up -d --build
docker compose logs -f habr-tg-bot
```

SQLite лежит в `./data`, модель Ollama лежит в Docker volume `ollama`.

## Частые Проблемы

Если бот не стартует, сначала смотрите логи:

```bash
docker compose logs -f habr-tg-bot
```

Если не скачалась модель:

```bash
docker compose logs ollama-pull
docker compose up -d ollama-pull
```

Если summary не генерируется, но статьи приходят, проверьте `LLM_BASE_URL`. В Docker должно быть `http://ollama:11434`, локально без Docker — `http://localhost:11434`.

Если `/latest` ничего не присылает, проверьте выбранные категории через `/my_categories`. Ещё можно временно снизить `MIN_ARTICLE_SCORE`.
