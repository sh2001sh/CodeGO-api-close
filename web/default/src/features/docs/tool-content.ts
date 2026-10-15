export type ToolCopy = {
  guide: string
  guideText: string
  guideSteps: string[]
  scripts: string
  scriptsText: string
  scriptsSteps: string[]
  codex: string
  codexText: string
  claude: string
  claudeText: string
  gemini: string
  geminiText: string
  opencode: string
  opencodeText: string
  openclaw: string
  openclawText: string
  hermes: string
  hermesText: string
  createKey: string
  downloadScript: string
  chooseModel: string
  toolNote: string
}

export const toolCopy: Record<string, ToolCopy> = {
  en: {
    guide: 'Product guide',
    guideText: 'Follow these steps before configuring a client.',
    guideSteps: [
      'Create an account and sign in.',
      'Create an API key in the console. Choose the group that contains the models you need.',
      'Check the model page, then send a test request or configure a local tool.',
    ],
    scripts: 'Download a setup script',
    scriptsText:
      'A ready-to-use setup script is available for Codex CLI from each API key. It contains your key, so store the download securely.',
    scriptsSteps: [
      'Create an API key, then open its actions menu.',
      'Select Download Codex Config Script and choose Windows or Linux/macOS.',
      'Review the downloaded script and run it on your own computer. It backs up your existing Codex configuration before writing config.toml and auth.json.',
      'Restart Codex, then select a model available to your key.',
    ],
    codex: 'Codex CLI',
    codexText:
      'From the API key list, download the Codex setup script for Windows or Linux/macOS. It configures the Responses provider and authentication. The provider block below shows the endpoint used by the script.',
    claude: 'Claude Code',
    claudeText:
      'Set the following values in Claude Code settings. The Anthropic base URL is the site origin without /v1.',
    gemini: 'Gemini CLI',
    geminiText:
      'Configure these environment values for Gemini CLI. The Gemini base URL is the site origin without /v1.',
    opencode: 'OpenCode',
    opencodeText:
      'Configure this provider with the OpenAI-compatible endpoint and a model available to your key.',
    openclaw: 'OpenClaw',
    openclawText: 'Configure this provider with the chat completions format.',
    hermes: 'Hermes',
    hermesText: 'Configure this provider with the chat completions format.',
    createKey: 'Create API key',
    downloadScript: 'Download Codex script',
    chooseModel: 'Browse models',
    toolNote:
      'Replace YOUR_API_KEY and MODEL_ID with your own values. Never share a real key in screenshots or support messages.',
  },
  zh: {
    guide: '使用指南',
    guideText: '配置客户端之前，先按以下顺序完成接入。',
    guideSteps: [
      '注册并登录账号。',
      '在控制台创建 API 密钥，选择包含所需模型的分组。',
      '在模型页确认可用模型，然后发送测试请求或配置本地工具。',
    ],
    scripts: '下载配置脚本',
    scriptsText:
      '每个 API 密钥都可以下载现成的 Codex CLI 配置脚本。脚本包含你的密钥，请妥善保存下载文件。',
    scriptsSteps: [
      '创建 API 密钥，然后打开该密钥的操作菜单。',
      '选择“下载 Codex 配置脚本”，再选择 Windows 或 Linux/macOS。',
      '检查下载的脚本，然后在自己的电脑上运行。脚本会先备份旧配置，再写入 config.toml 和 auth.json。',
      '重新启动 Codex，并选用当前密钥可用的模型。',
    ],
    codex: 'Codex CLI',
    codexText:
      '在 API 密钥列表下载 Windows 或 Linux/macOS 的 Codex 配置脚本。脚本会写入 Responses 服务商配置和认证信息。下方展示脚本使用的接口配置。',
    claude: 'Claude Code',
    claudeText:
      '在 Claude Code 设置中填写下列环境变量。Anthropic 接口地址是网站根地址，不带 /v1。',
    gemini: 'Gemini CLI',
    geminiText:
      '为 Gemini CLI 设置下列环境变量。Gemini 接口地址是网站根地址，不带 /v1。',
    opencode: 'OpenCode',
    opencodeText:
      '使用兼容 OpenAI 的接口地址配置此服务商，并选取当前密钥可用的模型。',
    openclaw: 'OpenClaw',
    openclawText: '使用聊天补全协议配置此服务商。',
    hermes: 'Hermes',
    hermesText: '使用聊天补全协议配置此服务商。',
    createKey: '创建 API 密钥',
    downloadScript: '下载 Codex 脚本',
    chooseModel: '查看模型',
    toolNote:
      '将 YOUR_API_KEY 和 MODEL_ID 换成自己的值。不要在截图或求助消息中公开真实密钥。',
  },
  'zh-TW': {
    guide: '使用指南',
    guideText: '設定用戶端之前，先依照以下順序完成接入。',
    guideSteps: [
      '註冊並登入帳號。',
      '在主控台建立 API 金鑰，選擇包含所需模型的分組。',
      '在模型頁確認可用模型，然後傳送測試請求或設定本機工具。',
    ],
    scripts: '下載設定指令碼',
    scriptsText:
      '每個 API 金鑰都可以下載現成的 Codex CLI 設定指令碼。指令碼包含你的金鑰，請妥善保存下載檔案。',
    scriptsSteps: [
      '建立 API 金鑰，然後開啟該金鑰的操作選單。',
      '選擇「下載 Codex 設定指令碼」，再選擇 Windows 或 Linux/macOS。',
      '檢查下載的指令碼，然後在自己的電腦上執行。指令碼會先備份舊設定，再寫入 config.toml 和 auth.json。',
      '重新啟動 Codex，並選用目前金鑰可用的模型。',
    ],
    codex: 'Codex CLI',
    codexText:
      '在 API 金鑰清單下載 Windows 或 Linux/macOS 的 Codex 設定指令碼。指令碼會寫入 Responses 服務供應商設定及驗證資訊。下方顯示其介面設定。',
    claude: 'Claude Code',
    claudeText:
      '在 Claude Code 設定中填入下列環境變數。Anthropic 介面位址是網站根位址，不含 /v1。',
    gemini: 'Gemini CLI',
    geminiText:
      '為 Gemini CLI 設定下列環境變數。Gemini 介面位址是網站根位址，不含 /v1。',
    opencode: 'OpenCode',
    opencodeText:
      '使用相容 OpenAI 的介面位址設定此服務供應商，並選取目前金鑰可用的模型。',
    openclaw: 'OpenClaw',
    openclawText: '使用聊天補全協定設定此服務供應商。',
    hermes: 'Hermes',
    hermesText: '使用聊天補全協定設定此服務供應商。',
    createKey: '建立 API 金鑰',
    downloadScript: '下載 Codex 指令碼',
    chooseModel: '查看模型',
    toolNote:
      '將 YOUR_API_KEY 和 MODEL_ID 換成自己的值。請勿在截圖或求助訊息中公開真實金鑰。',
  },
  fr: {
    guide: 'Guide produit',
    guideText: 'Suivez ces étapes avant de configurer un client.',
    guideSteps: [
      'Créez un compte et connectez-vous.',
      'Créez une clé API dans la console et choisissez le groupe contenant vos modèles.',
      'Vérifiez les modèles disponibles, puis envoyez une requête de test ou configurez un outil local.',
    ],
    scripts: 'Télécharger un script de configuration',
    scriptsText:
      'Un script prêt à utiliser est disponible pour Codex CLI depuis chaque clé API. Il contient votre clé : conservez le fichier en sécurité.',
    scriptsSteps: [
      'Créez une clé API, puis ouvrez son menu d’actions.',
      'Choisissez le script Codex pour Windows ou Linux/macOS.',
      'Vérifiez le script téléchargé et exécutez-le sur votre ordinateur. Il sauvegarde les réglages existants avant d’écrire config.toml et auth.json.',
      'Redémarrez Codex et choisissez un modèle accessible à votre clé.',
    ],
    codex: 'Codex CLI',
    codexText:
      'Téléchargez le script Codex pour Windows ou Linux/macOS depuis la liste des clés API. Il configure le fournisseur Responses et son authentification. Le bloc ci-dessous montre son point de terminaison.',
    claude: 'Claude Code',
    claudeText:
      'Définissez ces variables dans Claude Code. L’URL Anthropic est l’adresse du site sans /v1.',
    gemini: 'Gemini CLI',
    geminiText:
      'Définissez ces variables pour Gemini CLI. L’URL Gemini est l’adresse du site sans /v1.',
    opencode: 'OpenCode',
    opencodeText:
      'Configurez ce fournisseur avec le point de terminaison compatible OpenAI et un modèle accessible à votre clé.',
    openclaw: 'OpenClaw',
    openclawText: 'Configurez ce fournisseur au format chat completions.',
    hermes: 'Hermes',
    hermesText: 'Configurez ce fournisseur au format chat completions.',
    createKey: 'Créer une clé API',
    downloadScript: 'Télécharger le script Codex',
    chooseModel: 'Voir les modèles',
    toolNote:
      'Remplacez YOUR_API_KEY et MODEL_ID par vos valeurs. Ne publiez jamais une clé réelle dans une capture ou un message.',
  },
  ru: {
    guide: 'Руководство',
    guideText: 'Выполните эти шаги перед настройкой клиента.',
    guideSteps: [
      'Создайте учетную запись и войдите.',
      'Создайте ключ API в панели управления и выберите группу с нужными моделями.',
      'Проверьте доступные модели и отправьте тестовый запрос или настройте локальный инструмент.',
    ],
    scripts: 'Скачать скрипт настройки',
    scriptsText:
      'Готовый скрипт для Codex CLI доступен у каждого ключа API. Файл содержит ваш ключ: храните его безопасно.',
    scriptsSteps: [
      'Создайте ключ API и откройте меню действий этого ключа.',
      'Выберите скрипт Codex для Windows или Linux/macOS.',
      'Проверьте скачанный скрипт и запустите его на своем компьютере. Он сохранит прежние настройки перед записью config.toml и auth.json.',
      'Перезапустите Codex и выберите модель, доступную вашему ключу.',
    ],
    codex: 'Codex CLI',
    codexText:
      'Скачайте скрипт Codex для Windows или Linux/macOS из списка ключей API. Он настраивает провайдер Responses и авторизацию. Ниже показан адрес провайдера.',
    claude: 'Claude Code',
    claudeText:
      'Укажите эти переменные в настройках Claude Code. Адрес Anthropic указывается без /v1.',
    gemini: 'Gemini CLI',
    geminiText:
      'Укажите эти переменные для Gemini CLI. Адрес Gemini указывается без /v1.',
    opencode: 'OpenCode',
    opencodeText:
      'Настройте провайдера с совместимым с OpenAI адресом и моделью, доступной ключу.',
    openclaw: 'OpenClaw',
    openclawText: 'Настройте провайдера в формате chat completions.',
    hermes: 'Hermes',
    hermesText: 'Настройте провайдера в формате chat completions.',
    createKey: 'Создать ключ API',
    downloadScript: 'Скачать скрипт Codex',
    chooseModel: 'Просмотреть модели',
    toolNote:
      'Замените YOUR_API_KEY и MODEL_ID своими значениями. Не публикуйте реальный ключ в снимках экрана или сообщениях.',
  },
  ja: {
    guide: '利用ガイド',
    guideText: 'クライアントを設定する前に、次の手順を完了してください。',
    guideSteps: [
      'アカウントを作成してログインします。',
      'コンソールで API キーを作成し、必要なモデルを含むグループを選びます。',
      '利用可能なモデルを確認し、テストリクエストを送るかローカルツールを設定します。',
    ],
    scripts: '設定スクリプトをダウンロード',
    scriptsText:
      '各 API キーから Codex CLI 用の設定スクリプトをダウンロードできます。ファイルにはキーが含まれるため、安全に保管してください。',
    scriptsSteps: [
      'API キーを作成し、そのキーの操作メニューを開きます。',
      'Codex 設定スクリプトから Windows または Linux/macOS を選びます。',
      'ダウンロードしたスクリプトを確認してから自分のコンピューターで実行します。既存の設定をバックアップし、config.toml と auth.json を書き込みます。',
      'Codex を再起動し、キーで利用可能なモデルを選びます。',
    ],
    codex: 'Codex CLI',
    codexText:
      'API キー一覧から Windows または Linux/macOS 向けの Codex 設定スクリプトをダウンロードします。Responses プロバイダーと認証が設定されます。以下は使用する接続先です。',
    claude: 'Claude Code',
    claudeText:
      'Claude Code に次の環境変数を設定します。Anthropic の URL に /v1 は付けません。',
    gemini: 'Gemini CLI',
    geminiText:
      'Gemini CLI に次の環境変数を設定します。Gemini の URL に /v1 は付けません。',
    opencode: 'OpenCode',
    opencodeText:
      'OpenAI 互換の接続先と利用可能なモデルを持つプロバイダーを設定します。',
    openclaw: 'OpenClaw',
    openclawText: 'chat completions 形式のプロバイダーを設定します。',
    hermes: 'Hermes',
    hermesText: 'chat completions 形式のプロバイダーを設定します。',
    createKey: 'API キーを作成',
    downloadScript: 'Codex スクリプトをダウンロード',
    chooseModel: 'モデルを見る',
    toolNote:
      'YOUR_API_KEY と MODEL_ID を自分の値に置き換えてください。実際のキーを画像やメッセージに載せないでください。',
  },
  vi: {
    guide: 'Hướng dẫn',
    guideText: 'Hoàn thành các bước này trước khi cấu hình ứng dụng.',
    guideSteps: [
      'Tạo tài khoản và đăng nhập.',
      'Tạo khóa API trong bảng điều khiển và chọn nhóm chứa các mô hình cần dùng.',
      'Kiểm tra mô hình khả dụng, sau đó gửi yêu cầu thử hoặc cấu hình công cụ cục bộ.',
    ],
    scripts: 'Tải tập lệnh cấu hình',
    scriptsText:
      'Bạn có thể tải tập lệnh cấu hình Codex CLI từ mỗi khóa API. Tệp chứa khóa của bạn, hãy lưu trữ an toàn.',
    scriptsSteps: [
      'Tạo khóa API rồi mở menu thao tác của khóa đó.',
      'Chọn tập lệnh Codex cho Windows hoặc Linux/macOS.',
      'Kiểm tra tập lệnh đã tải rồi chạy trên máy của bạn. Tập lệnh sao lưu cấu hình cũ trước khi ghi config.toml và auth.json.',
      'Khởi động lại Codex và chọn mô hình mà khóa của bạn có thể dùng.',
    ],
    codex: 'Codex CLI',
    codexText:
      'Tải tập lệnh cấu hình Codex cho Windows hoặc Linux/macOS từ danh sách khóa API. Tập lệnh cấu hình nhà cung cấp Responses và xác thực. Khối bên dưới cho biết địa chỉ kết nối.',
    claude: 'Claude Code',
    claudeText:
      'Đặt các biến sau trong Claude Code. URL Anthropic là địa chỉ gốc, không có /v1.',
    gemini: 'Gemini CLI',
    geminiText:
      'Đặt các biến sau cho Gemini CLI. URL Gemini là địa chỉ gốc, không có /v1.',
    opencode: 'OpenCode',
    opencodeText:
      'Cấu hình nhà cung cấp với địa chỉ tương thích OpenAI và mô hình mà khóa có quyền dùng.',
    openclaw: 'OpenClaw',
    openclawText: 'Cấu hình nhà cung cấp ở định dạng chat completions.',
    hermes: 'Hermes',
    hermesText: 'Cấu hình nhà cung cấp ở định dạng chat completions.',
    createKey: 'Tạo khóa API',
    downloadScript: 'Tải tập lệnh Codex',
    chooseModel: 'Xem mô hình',
    toolNote:
      'Thay YOUR_API_KEY và MODEL_ID bằng giá trị của bạn. Đừng chia sẻ khóa thật trong ảnh chụp hay tin nhắn.',
  },
}

export const toolExamples = {
  codex: `model_provider = "CodeGo"\nmodel = "MODEL_ID"\n\n[model_providers.CodeGo]\nname = "CodeGo"\nbase_url = "https://codegoai.com/v1"\nwire_api = "responses"\nrequires_openai_auth = true\nsupports_websockets = true`,
  codexAuth: `{"OPENAI_API_KEY":"YOUR_API_KEY"}`,
  claude: `{\n  "env": {\n    "ANTHROPIC_BASE_URL": "https://codegoai.com",\n    "ANTHROPIC_AUTH_TOKEN": "YOUR_API_KEY",\n    "ANTHROPIC_MODEL": "MODEL_ID"\n  }\n}`,
  gemini: `GEMINI_API_KEY=YOUR_API_KEY\nGOOGLE_GEMINI_BASE_URL=https://codegoai.com\nGEMINI_MODEL=MODEL_ID`,
  opencode: `{\n  "npm": "@ai-sdk/openai-compatible",\n  "options": {\n    "baseURL": "https://codegoai.com/v1",\n    "apiKey": "YOUR_API_KEY"\n  },\n  "models": { "MODEL_ID": { "name": "MODEL_ID" } }\n}`,
  openclaw: `{\n  "baseUrl": "https://codegoai.com/v1",\n  "apiKey": "YOUR_API_KEY",\n  "api": "openai-completions",\n  "models": [{ "id": "MODEL_ID", "name": "MODEL_ID" }]\n}`,
  hermes: `{\n  "base_url": "https://codegoai.com/v1",\n  "api_key": "YOUR_API_KEY",\n  "api_mode": "chat_completions",\n  "models": [{ "id": "MODEL_ID", "name": "MODEL_ID" }]\n}`,
}
