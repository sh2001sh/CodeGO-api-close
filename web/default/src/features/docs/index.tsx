import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { normalizeInterfaceLanguage } from '@/i18n/languages'
import { Check, Copy, KeyRound, List, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SiteSeo } from '@/components/seo'
import { DawnNav } from '@/features/dawn/components/dawn-nav'
import './styles.css'
import { toolCopy, toolExamples } from './tool-content'

type Copy = {
  title: string
  subtitle: string
  search: string
  contents: string
  base: string
  baseText: string
  key: string
  keyText: string
  models: string
  modelsText: string
  chat: string
  chatText: string
  responses: string
  responsesText: string
  errors: string
  errorsText: string
  copy: string
  copied: string
  guide: string
  pricing: string
}

const copy: Record<string, Copy> = {
  en: {
    title: 'API documentation',
    subtitle: 'Connect your application to Code Go.',
    search: 'Search sections',
    contents: 'On this page',
    base: 'Base URL',
    baseText: 'Use this HTTPS address for every API request.',
    key: 'Authentication',
    keyText:
      'Create an API key in the console. Send it in the Authorization header. Keep it on your server and never expose it in a browser.',
    models: 'List models',
    modelsText:
      'Fetch the models available to your key. Choose a model ID from the response or the models page.',
    chat: 'Chat completions',
    chatText:
      'Send a conversation as messages. The model ID must be available to your key.',
    responses: 'Responses',
    responsesText: 'Use the Responses API for compatible models and clients.',
    errors: 'Errors and limits',
    errorsText:
      'A non-2xx response indicates a failed request. Check the status and error body. Rate limits and model access depend on your key and plan.',
    copy: 'Copy code',
    copied: 'Copied',
    guide: 'Product guide',
    pricing: 'Browse models',
  },
  zh: {
    title: 'API 文档',
    subtitle: '将应用接入 Code Go。',
    search: '搜索章节',
    contents: '本页目录',
    base: '接口地址',
    baseText: '所有 API 请求均使用以下 HTTPS 地址。',
    key: '身份验证',
    keyText:
      '在控制台创建 API 密钥，并通过 Authorization 请求头传递。密钥应保存在服务端，不要暴露给浏览器。',
    models: '查询模型',
    modelsText: '查询当前密钥可用的模型。从响应或模型页面选择模型 ID。',
    chat: '聊天补全',
    chatText: '以 messages 形式发送对话。模型 ID 须对当前密钥开放。',
    responses: 'Responses 接口',
    responsesText: '兼容的模型和客户端可使用 Responses 接口。',
    errors: '错误与限制',
    errorsText:
      '非 2xx 状态表示请求失败。请查看状态码与错误响应。速率限制和模型权限取决于密钥与套餐。',
    copy: '复制代码',
    copied: '已复制',
    guide: '使用指南',
    pricing: '查看模型',
  },
  'zh-TW': {
    title: 'API 文件',
    subtitle: '將應用程式接入 Code Go。',
    search: '搜尋章節',
    contents: '本頁目錄',
    base: '介面位址',
    baseText: '所有 API 請求均使用以下 HTTPS 位址。',
    key: '身分驗證',
    keyText:
      '在主控台建立 API 金鑰，並透過 Authorization 請求標頭傳遞。金鑰應儲存在伺服器端，不要暴露於瀏覽器。',
    models: '查詢模型',
    modelsText: '查詢目前金鑰可用的模型。從回應或模型頁面選擇模型 ID。',
    chat: '聊天補全',
    chatText: '以 messages 形式傳送對話。模型 ID 須對目前金鑰開放。',
    responses: 'Responses 介面',
    responsesText: '相容的模型與用戶端可使用 Responses 介面。',
    errors: '錯誤與限制',
    errorsText:
      '非 2xx 狀態表示請求失敗。請查看狀態碼與錯誤回應。速率限制與模型權限取決於金鑰及方案。',
    copy: '複製程式碼',
    copied: '已複製',
    guide: '使用指南',
    pricing: '查看模型',
  },
  fr: {
    title: 'Documentation API',
    subtitle: 'Connectez votre application à Code Go.',
    search: 'Rechercher une section',
    contents: 'Sur cette page',
    base: 'URL de base',
    baseText: 'Utilisez cette adresse HTTPS pour chaque requête API.',
    key: 'Authentification',
    keyText:
      'Créez une clé API dans la console et transmettez-la dans l’en-tête Authorization. Conservez-la sur votre serveur.',
    models: 'Lister les modèles',
    modelsText:
      'Récupérez les modèles accessibles avec votre clé. Choisissez un identifiant dans la réponse ou sur la page des modèles.',
    chat: 'Complétions de chat',
    chatText:
      'Envoyez la conversation sous forme de messages avec un modèle accessible à votre clé.',
    responses: 'API Responses',
    responsesText:
      'Utilisez cette API avec les modèles et clients compatibles.',
    errors: 'Erreurs et limites',
    errorsText:
      'Une réponse hors 2xx indique un échec. Vérifiez le code et le corps de la réponse. Les limites dépendent de la clé et de votre offre.',
    copy: 'Copier le code',
    copied: 'Copié',
    guide: 'Guide produit',
    pricing: 'Voir les modèles',
  },
  ru: {
    title: 'Документация API',
    subtitle: 'Подключите приложение к Code Go.',
    search: 'Поиск разделов',
    contents: 'На этой странице',
    base: 'Базовый URL',
    baseText: 'Используйте этот HTTPS-адрес для всех запросов API.',
    key: 'Аутентификация',
    keyText:
      'Создайте ключ API в панели управления и передавайте его в заголовке Authorization. Храните ключ на сервере.',
    models: 'Список моделей',
    modelsText:
      'Получите модели, доступные вашему ключу. Выберите ID из ответа или на странице моделей.',
    chat: 'Чат-запросы',
    chatText:
      'Передайте диалог в поле messages и укажите модель, доступную вашему ключу.',
    responses: 'API Responses',
    responsesText: 'Используйте этот API с совместимыми моделями и клиентами.',
    errors: 'Ошибки и ограничения',
    errorsText:
      'Ответ вне диапазона 2xx означает ошибку. Проверьте код и тело ответа. Ограничения зависят от ключа и тарифа.',
    copy: 'Копировать код',
    copied: 'Скопировано',
    guide: 'Руководство',
    pricing: 'Модели',
  },
  ja: {
    title: 'API ドキュメント',
    subtitle: 'アプリケーションを Code Go に接続します。',
    search: 'セクションを検索',
    contents: 'このページの内容',
    base: 'ベース URL',
    baseText: 'すべての API リクエストにこの HTTPS アドレスを使用します。',
    key: '認証',
    keyText:
      'コンソールで API キーを作成し、Authorization ヘッダーで送信します。キーはサーバーに保管してください。',
    models: 'モデル一覧',
    modelsText:
      'キーで利用可能なモデルを取得します。レスポンスまたはモデルページから ID を選びます。',
    chat: 'チャット補完',
    chatText:
      '会話を messages で送信します。キーで利用できるモデルを指定してください。',
    responses: 'Responses API',
    responsesText: '対応するモデルとクライアントでこの API を利用できます。',
    errors: 'エラーと制限',
    errorsText:
      '2xx 以外は失敗です。ステータスとエラー本文を確認してください。制限はキーとプランによって異なります。',
    copy: 'コードをコピー',
    copied: 'コピーしました',
    guide: '利用ガイド',
    pricing: 'モデルを見る',
  },
  vi: {
    title: 'Tài liệu API',
    subtitle: 'Kết nối ứng dụng của bạn với Code Go.',
    search: 'Tìm mục',
    contents: 'Trong trang này',
    base: 'URL cơ sở',
    baseText: 'Dùng địa chỉ HTTPS này cho mọi yêu cầu API.',
    key: 'Xác thực',
    keyText:
      'Tạo khóa API trong bảng điều khiển và gửi qua tiêu đề Authorization. Giữ khóa trên máy chủ của bạn.',
    models: 'Danh sách mô hình',
    modelsText:
      'Lấy các mô hình mà khóa của bạn có thể sử dụng. Chọn ID từ phản hồi hoặc trang mô hình.',
    chat: 'Hoàn thành hội thoại',
    chatText:
      'Gửi hội thoại qua messages với mô hình mà khóa của bạn có quyền dùng.',
    responses: 'API Responses',
    responsesText: 'Dùng API này với mô hình và ứng dụng khách tương thích.',
    errors: 'Lỗi và giới hạn',
    errorsText:
      'Phản hồi ngoài 2xx cho biết yêu cầu thất bại. Kiểm tra mã trạng thái và nội dung lỗi. Giới hạn tùy thuộc vào khóa và gói.',
    copy: 'Sao chép mã',
    copied: 'Đã sao chép',
    guide: 'Hướng dẫn',
    pricing: 'Xem mô hình',
  },
}

const examples = {
  base: 'https://codegoai.com/v1',
  key: 'Authorization: Bearer YOUR_API_KEY',
  models:
    'curl https://codegoai.com/v1/models -H "Authorization: Bearer YOUR_API_KEY"',
  chat: `curl https://codegoai.com/v1/chat/completions -H "Authorization: Bearer YOUR_API_KEY" -H "Content-Type: application/json" -d '{"model":"MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'`,
  responses: `curl https://codegoai.com/v1/responses -H "Authorization: Bearer YOUR_API_KEY" -H "Content-Type: application/json" -d '{"model":"MODEL_ID","input":"Hello"}'`,
}

const sectionExamples = { ...examples, ...toolExamples }
const exampleLabels: Record<keyof typeof sectionExamples, string> = {
  base: 'HTTPS',
  key: 'HTTP header',
  models: 'cURL',
  chat: 'cURL',
  responses: 'cURL',
  codex: '~/.codex/config.toml',
  codexAuth: '~/.codex/auth.json',
  claude: 'Claude Code settings.json',
  gemini: 'Environment',
  opencode: 'OpenCode provider',
  openclaw: 'OpenClaw provider',
  hermes: 'Hermes provider',
}

type SectionId =
  | 'guide'
  | 'scripts'
  | 'codex'
  | 'claude'
  | 'gemini'
  | 'opencode'
  | 'openclaw'
  | 'hermes'
  | 'base'
  | 'key'
  | 'models'
  | 'chat'
  | 'responses'
  | 'errors'
const sectionIds: SectionId[] = [
  'guide',
  'scripts',
  'codex',
  'claude',
  'gemini',
  'opencode',
  'openclaw',
  'hermes',
  'base',
  'key',
  'models',
  'chat',
  'responses',
  'errors',
]

function CodeBlock({
  value,
  labels,
  name,
}: {
  value: string
  labels: Copy
  name: string
}) {
  const [copied, setCopied] = useState(false)
  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1800)
    } catch {
      setCopied(false)
    }
  }
  return (
    <div className='docs-code'>
      <div className='docs-code-name'>{name}</div>
      <button
        type='button'
        onClick={handleCopy}
        aria-label={copied ? labels.copied : labels.copy}
        title={copied ? labels.copied : labels.copy}
      >
        {copied ? <Check size={16} /> : <Copy size={16} />}
      </button>
      <pre>
        <code>{value}</code>
      </pre>
    </div>
  )
}

export function ApiDocs() {
  const { i18n } = useTranslation()
  const locale = normalizeInterfaceLanguage(i18n.language)
  const labels = { ...copy[locale], ...toolCopy[locale] }
  const [query, setQuery] = useState('')
  const filtered = sectionIds.filter((id) =>
    `${labels[id]} ${labels[`${id}Text` as keyof typeof labels]}`
      .toLocaleLowerCase()
      .includes(query.toLocaleLowerCase())
  )

  return (
    <div className='dawn docs-page'>
      <SiteSeo
        title={labels.title}
        description={labels.subtitle}
        canonicalPath='/docs'
      />
      <DawnNav />
      <div className='docs-shell'>
        <aside className='docs-sidebar' aria-label={labels.contents}>
          <div className='docs-search'>
            <Search size={16} />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={labels.search}
              aria-label={labels.search}
            />
          </div>
          <nav>
            {filtered.map((id) => (
              <a key={id} href={`#${id}`}>
                {labels[id]}
              </a>
            ))}
          </nav>
          <div className='docs-side-links'>
            <Link to='/pricing'>{labels.pricing}</Link>
          </div>
        </aside>
        <main className='docs-main'>
          <div className='docs-mobile-nav'>
            <List size={16} />
            <select
              aria-label={labels.contents}
              onChange={(event) =>
                document.getElementById(event.target.value)?.scrollIntoView()
              }
              defaultValue=''
            >
              <option value='' disabled>
                {labels.contents}
              </option>
              {sectionIds.map((id) => (
                <option key={id} value={id}>
                  {labels[id]}
                </option>
              ))}
            </select>
          </div>
          <header className='docs-heading'>
            <span>
              <KeyRound size={17} /> Code Go API
            </span>
            <h1>{labels.title}</h1>
            <p>{labels.subtitle}</p>
          </header>
          {sectionIds.map((id) => (
            <section className='docs-section' id={id} key={id}>
              <h2>{labels[id]}</h2>
              <p>{labels[`${id}Text` as keyof typeof labels]}</p>
              {(id === 'guide' || id === 'scripts') && (
                <ol className='docs-steps'>
                  {labels[`${id}Steps`].map((step) => (
                    <li key={step}>{step}</li>
                  ))}
                </ol>
              )}
              {id === 'guide' && (
                <div className='docs-actions'>
                  <Link to='/keys'>{labels.createKey}</Link>
                  <Link to='/pricing'>{labels.chooseModel}</Link>
                </div>
              )}
              {id === 'scripts' && (
                <div className='docs-actions'>
                  <Link to='/keys'>{labels.downloadScript}</Link>
                </div>
              )}
              {id === 'codex' && (
                <div className='docs-actions'>
                  <Link to='/keys'>{labels.createKey}</Link>
                </div>
              )}
              {id in sectionExamples && (
                <CodeBlock
                  value={sectionExamples[id as keyof typeof sectionExamples]}
                  labels={labels}
                  name={exampleLabels[id as keyof typeof exampleLabels]}
                />
              )}
              {id === 'codex' && (
                <CodeBlock
                  value={toolExamples.codexAuth}
                  labels={labels}
                  name={exampleLabels.codexAuth}
                />
              )}
              {id in toolExamples && (
                <p className='docs-note'>{labels.toolNote}</p>
              )}
            </section>
          ))}
        </main>
        <aside className='docs-toc' aria-label={labels.contents}>
          <strong>{labels.contents}</strong>
          {sectionIds.map((id) => (
            <a key={id} href={`#${id}`}>
              {labels[id]}
            </a>
          ))}
        </aside>
      </div>
    </div>
  )
}
