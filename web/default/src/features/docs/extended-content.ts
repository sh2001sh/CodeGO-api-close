import { COMMUNITY_LINKS } from '../../lib/company'
import type { DocArticle, DocBlock, Text } from './content'

// Keep each passage together so missing translations fail type checking.
const text = (
  cn: string,
  hk: string,
  en: string,
  fr: string,
  ru: string,
  ja: string,
  vi: string
): Text => ({
  'zh-CN': cn,
  'zh-HK': hk,
  en,
  fr,
  ru,
  ja,
  vi,
})
const p = (copy: Text): DocBlock => ({ kind: 'paragraph', text: copy })

export const serviceTierSection: DocArticle['sections'][number] = {
  id: 'fast-mode',
  title: text(
    'Fast、UltraFast 与计费公式',
    'Fast、UltraFast 與計費公式',
    'Fast, UltraFast and billing formulas',
    'Fast, UltraFast et calcul des frais',
    'Fast, UltraFast и расчёт стоимости',
    'Fast・UltraFast と料金の計算',
    'Fast, UltraFast và công thức tính phí'
  ),
  blocks: [
    p(
      text(
        'Codex Fast 使用 service_tier: "priority"，本站按 2 倍计费；UltraFast 使用 "ultrafast"，本站按 6 倍计费。不要发送 "fast"。6 倍为本站当前计费配置，本文不代表已独立核实上游官方定价。能否提供加速取决于模型、渠道与上游容量。',
        'Codex Fast 使用 service_tier: "priority"，本站按 2 倍計費；UltraFast 使用 "ultrafast"，本站按 6 倍計費。請勿傳送 "fast"。6 倍為本站目前計費設定，本文不代表已獨立核實上游官方定價。能否加速取決於模型、渠道及上游容量。',
        'Codex Fast uses service_tier: "priority" and costs 2× on this site; UltraFast uses "ultrafast" and costs 6×. Do not send "fast". The 6× rate is this site’s current configuration, not an independently verified statement of upstream pricing. Acceleration depends on the model, channel and upstream capacity.',
        'Codex Fast utilise service_tier: "priority" et coûte 2× ici ; UltraFast utilise "ultrafast" et coûte 6×. N’envoyez pas "fast". Le tarif 6× est la configuration actuelle du site, sans vérification indépendante du tarif amont. L’accélération dépend du modèle, du canal et de la capacité amont.',
        'Codex Fast использует service_tier: "priority" и стоит здесь 2×; UltraFast использует "ultrafast" и стоит 6×. Не передавайте "fast". Множитель 6× — текущая настройка сайта, а не независимо подтверждённая цена поставщика. Ускорение зависит от модели, канала и мощности поставщика.',
        'Codex Fast は service_tier: "priority" で本サイトでは2倍、UltraFast は "ultrafast" で6倍です。"fast" は送信しないでください。6倍は本サイトの現在の設定で、上流の公式料金を独自に確認したという意味ではありません。高速化はモデル、チャネル、上流の処理能力に依存します。',
        'Codex Fast dùng service_tier: "priority", tính phí 2× tại đây; UltraFast dùng "ultrafast", tính 6×. Không gửi "fast". Mức 6× là cấu hình hiện tại của trang, không phải xác nhận độc lập về giá chính thức của nhà cung cấp. Tốc độ phụ thuộc mô hình, kênh và năng lực phía trên.'
      )
    ),
    p(
      text(
        '计费顺序为：按实际用量计算基础费用（含分组价格和适用缓存价格）→ 乘服务档位倍率一次 → 应用适用折扣 → 按资金来源换算及舍入。分组报价已经包含分组倍率，不要重复乘算。例：基础费用 0.01 credits、无折扣时，Fast 为 0.02，UltraFast 为 0.06。',
        '計費順序為：按實際用量計算基礎費用（含分組價格及適用快取價格）→ 乘服務層級倍率一次 → 套用適用折扣 → 按資金來源換算及四捨五入。分組報價已包含分組倍率，請勿重複乘算。例如基礎費用 0.01 credits、無折扣時，Fast 為 0.02，UltraFast 為 0.06。',
        'Calculation order: base charge from actual usage, including group and applicable cache prices → apply the service-tier multiplier once → apply eligible discounts → convert and round for the funding source. Published group prices already include the group multiplier. For a base charge of 0.01 credits without discounts, Fast costs 0.02 and UltraFast 0.06.',
        'Ordre de calcul : coût de base selon l’usage réel, prix du groupe et du cache inclus → multiplicateur du niveau une seule fois → réductions applicables → conversion et arrondi selon le financement. Les prix du groupe incluent déjà son multiplicateur. Pour 0,01 credit de base sans réduction : Fast coûte 0,02, UltraFast 0,06.',
        'Порядок: базовая стоимость фактического использования с ценами группы и кэша → один раз множитель уровня → применимые скидки → пересчёт и округление для источника оплаты. Цена группы уже включает её множитель. При базе 0,01 credits без скидки Fast стоит 0,02, UltraFast — 0,06.',
        '計算順序は、実使用量から基本料金を計算（グループ価格と適用されるキャッシュ価格を含む）→ サービス階層の倍率を1回適用 → 対象の割引を適用 → 支払元に応じて換算・丸め、です。表示価格にはグループ倍率が含まれます。割引なしで基本料金が0.01 creditsなら、Fastは0.02、UltraFastは0.06です。',
        'Thứ tự tính: phí cơ sở theo lượng dùng thực tế, gồm giá nhóm và bộ nhớ đệm áp dụng → nhân hệ số cấp dịch vụ một lần → áp dụng ưu đãi phù hợp → quy đổi và làm tròn theo nguồn thanh toán. Giá nhóm đã gồm hệ số nhóm. Phí cơ sở 0,01 credits không giảm giá thì Fast là 0,02, UltraFast là 0,06.'
      )
    ),
    { kind: 'code', sample: 'responses-fast' },
    { kind: 'code', sample: 'responses-ultrafast' },
    p(
      text(
        '在使用日志详情核对 service_tier、service_tier_multiplier 和 billing_calculation。预扣不等于最终扣费；以最终用量、实际结算档位、退款和资金来源记录核对。开启加速不保证固定延迟，也不保证请求一定成功。',
        '在使用日誌詳情核對 service_tier、service_tier_multiplier 及 billing_calculation。預扣並非最終扣費；請按最終用量、實際結算層級、退款及資金來源記錄核對。啟用加速不保證固定延遲或請求成功。',
        'Check service_tier, service_tier_multiplier and billing_calculation in usage-log details. A reservation is not the final charge; reconcile final usage, the settled tier, refunds and funding-source records. Acceleration guarantees neither a fixed latency nor request success.',
        'Vérifiez service_tier, service_tier_multiplier et billing_calculation dans les journaux. Une réservation n’est pas le débit final : rapprochez usage final, niveau facturé, remboursements et source de financement. L’accélération ne garantit ni délai fixe ni succès.',
        'Проверьте service_tier, service_tier_multiplier и billing_calculation в журнале. Резерв не равен окончательному списанию: сверяйте итоговое использование, уровень расчёта, возвраты и источник оплаты. Ускорение не гарантирует задержку или успех запроса.',
        '使用ログの詳細で service_tier、service_tier_multiplier、billing_calculation を確認してください。仮確保額は最終料金ではありません。最終使用量、精算階層、返金、支払元の記録を照合します。高速化は一定の遅延や成功を保証しません。',
        'Kiểm tra service_tier, service_tier_multiplier và billing_calculation trong chi tiết nhật ký. Khoản tạm giữ không phải phí cuối cùng; đối chiếu lượng dùng cuối, cấp dịch vụ đã tính, hoàn trả và nguồn thanh toán. Tăng tốc không bảo đảm độ trễ cố định hay yêu cầu thành công.'
      )
    ),
  ],
}

export const extendedArticles: readonly DocArticle[] = [
  {
    slug: 'request-parameters',
    group: 'api',
    title: text(
      '请求参数与上下文',
      '請求參數與上下文',
      'Request parameters and context',
      'Paramètres et contexte',
      'Параметры и контекст',
      'リクエスト引数とコンテキスト',
      'Tham số và ngữ cảnh'
    ),
    summary: text(
      '区分协议参数、模型限制与通用网关能力。',
      '區分協定參數、模型限制與通用閘道能力。',
      'Distinguish protocol parameters, model limits and gateway support.',
      'Distinguer protocole, limites du modèle et passerelle.',
      'Различайте протокол, лимиты модели и возможности шлюза.',
      'プロトコル引数、モデル制限、ゲートウェイ対応を区別します。',
      'Phân biệt tham số giao thức, giới hạn mô hình và hỗ trợ cổng.'
    ),
    sections: [
      {
        id: 'fields',
        title: text(
          '常用字段',
          '常用欄位',
          'Common fields',
          'Champs courants',
          'Основные поля',
          '主なフィールド',
          'Các trường thường dùng'
        ),
        blocks: [
          p(
            text(
              'Chat Completions 使用 model 和 messages；多轮对话须按顺序带上历史 user、assistant、tool 消息。Responses 使用 model 和 input。嵌入接口使用 model 和 input，结果是向量而非聊天文本。不要把一种协议的消息结构直接传给另一种。',
              'Chat Completions 使用 model 與 messages；多輪對話須按順序附上歷史 user、assistant、tool 訊息。Responses 使用 model 與 input。嵌入介面使用 model 與 input，結果是向量而非聊天文字。請勿將一種協定的訊息結構直接傳給另一種。',
              'Chat Completions takes model and messages; include prior user, assistant and tool messages in order for multi-turn conversations. Responses takes model and input. Embeddings takes model and input and returns vectors, not chat text. Do not mix message formats between protocols.',
              'Chat Completions reçoit model et messages ; joignez dans l’ordre les messages user, assistant et tool précédents. Responses reçoit model et input. Embeddings reçoit model et input et renvoie des vecteurs. Ne mélangez pas les formats des protocoles.',
              'Chat Completions принимает model и messages; для диалога передавайте предыдущие сообщения user, assistant и tool по порядку. Responses принимает model и input. Embeddings возвращает векторы, а не текст. Не смешивайте форматы протоколов.',
              'Chat Completionsはmodelとmessagesを使い、複数ターンでは過去のuser、assistant、toolを順番に含めます。Responsesはmodelとinputです。Embeddingsもmodelとinputを使いますが、結果は文章ではなくベクトルです。異なるプロトコルの形式を混ぜないでください。',
              'Chat Completions nhận model và messages; đối thoại nhiều lượt cần kèm lịch sử user, assistant, tool theo thứ tự. Responses nhận model và input. Embeddings trả vectơ thay vì văn bản trò chuyện. Không trộn định dạng thông điệp giữa các giao thức.'
            )
          ),
          p(
            text(
              'temperature 和 top_p 控制采样，通常只调整其一；某些推理模型不接受它们。Chat Completions 的输出上限按模型选用 max_tokens 或 max_completion_tokens；Responses 使用 max_output_tokens。上下文限制通常包含输入、历史、工具定义和输出预算，推理 token 也可能占用预算。',
              'temperature 及 top_p 控制取樣，通常僅調整其中一項；部分推理模型不接受。Chat Completions 按模型選用 max_tokens 或 max_completion_tokens；Responses 使用 max_output_tokens。上下文限制通常包含輸入、歷史、工具定義及輸出預算，推理 token 亦可能佔用預算。',
              'temperature and top_p control sampling; normally tune one, and some reasoning models reject them. For Chat Completions, use max_tokens or max_completion_tokens as required by the model; Responses uses max_output_tokens. Context limits generally include input, history, tool definitions and output budget; reasoning tokens may consume that budget.',
              'temperature et top_p règlent l’échantillonnage ; ajustez-en généralement un seul. Certains modèles de raisonnement les refusent. Chat Completions utilise max_tokens ou max_completion_tokens selon le modèle ; Responses utilise max_output_tokens. Le contexte comprend généralement entrée, historique, outils et budget de sortie, parfois raisonnement inclus.',
              'temperature и top_p управляют выборкой; обычно меняют один параметр, а некоторые модели рассуждений их не принимают. В Chat Completions используйте max_tokens или max_completion_tokens согласно модели; в Responses — max_output_tokens. Контекст обычно включает вход, историю, определения инструментов и бюджет вывода, включая возможные токены рассуждений.',
              'temperatureとtop_pはサンプリングを制御し、通常は片方だけ調整します。推論モデルによっては使用できません。Chat Completionsはモデルに応じてmax_tokensまたはmax_completion_tokens、Responsesはmax_output_tokensです。入力、履歴、ツール定義、出力予算がコンテキスト制限に入り、推論トークンも予算を使う場合があります。',
              'temperature và top_p điều khiển lấy mẫu, thường chỉ chỉnh một; một số mô hình suy luận không nhận chúng. Chat Completions dùng max_tokens hoặc max_completion_tokens tùy mô hình; Responses dùng max_output_tokens. Ngữ cảnh thường gồm đầu vào, lịch sử, định nghĩa công cụ và ngân sách đầu ra, có thể cả token suy luận.'
            )
          ),
          p(
            text(
              'stream=true 启用流式；stream_options.include_usage 请求末尾用量，具体上游仍可能不提供。tools、tool_choice、response_format、reasoning_effort 和 service_tier 均须确认模型及渠道支持。收到不支持参数错误时移除可选字段逐项测试，不要假设“网关接受 JSON”就代表模型执行了该参数。',
              'stream=true 啟用串流；stream_options.include_usage 請求末尾用量，個別上游仍可能不提供。tools、tool_choice、response_format、reasoning_effort 及 service_tier 須確認模型與渠道支援。遇到不支援參數錯誤時，移除選填欄位逐項測試；閘道接受 JSON 並不代表模型執行該參數。',
              'stream=true enables streaming; stream_options.include_usage requests final usage, which some upstreams may omit. Check model and channel support for tools, tool_choice, response_format, reasoning_effort and service_tier. On unsupported-parameter errors, remove optional fields and test individually; accepting JSON does not prove a model honored a parameter.',
              'stream=true active le flux ; stream_options.include_usage demande l’usage final, parfois absent en amont. Vérifiez tools, tool_choice, response_format, reasoning_effort et service_tier pour le modèle et le canal. En cas de paramètre non pris en charge, retirez les options puis testez-les séparément ; accepter le JSON ne prouve pas leur exécution.',
              'stream=true включает поток; stream_options.include_usage запрашивает итоговое использование, которое поставщик может не вернуть. Проверяйте поддержку tools, tool_choice, response_format, reasoning_effort и service_tier. При ошибках уберите необязательные поля и проверяйте отдельно: приём JSON не доказывает применение параметра моделью.',
              'stream=trueでストリームを有効にし、stream_options.include_usageで最終使用量を要求しますが、上流によっては届きません。tools、tool_choice、response_format、reasoning_effort、service_tierは対応を確認してください。非対応エラーなら任意引数を外して個別に検証します。JSONの受理は引数の適用を証明しません。',
              'stream=true bật truyền luồng; stream_options.include_usage yêu cầu lượng dùng cuối nhưng phía trên có thể không trả. Kiểm tra hỗ trợ tools, tool_choice, response_format, reasoning_effort và service_tier. Nếu lỗi không hỗ trợ, bỏ trường tùy chọn rồi thử từng mục; nhận JSON không chứng minh mô hình đã áp dụng tham số.'
            )
          ),
        ],
      },
    ],
  },
  {
    slug: 'sdk-streaming',
    group: 'api',
    title: text(
      'SDK 流式调用',
      'SDK 串流呼叫',
      'Streaming with SDKs',
      'Flux avec les SDK',
      'Потоковая работа с SDK',
      'SDKでストリーミング',
      'Truyền luồng bằng SDK'
    ),
    summary: text(
      'Python、Node.js 与 cURL 的完整流式示例。',
      'Python、Node.js 與 cURL 的完整串流範例。',
      'Complete streaming examples for Python, Node.js and cURL.',
      'Exemples complets en Python, Node.js et cURL.',
      'Полные примеры для Python, Node.js и cURL.',
      'Python・Node.js・cURLのストリーミング例です。',
      'Ví dụ đầy đủ cho Python, Node.js và cURL.'
    ),
    sections: [
      {
        id: 'stream',
        title: text(
          '读取增量与用量',
          '讀取增量及用量',
          'Read deltas and usage',
          'Lire les fragments et l’usage',
          'Чтение фрагментов и использования',
          '差分と使用量を読む',
          'Đọc phần tăng thêm và lượng dùng'
        ),
        blocks: [
          p(
            text(
              'Python 安装命令为 pip install -U openai；Node.js 为 npm install openai（也可 bun add openai）。在服务器环境设置 CODEGO_API_KEY。以下 cURL 示例使用 Bash 语法，Windows 请用 WSL/Git Bash，或选择 Python/Node.js。超时 60 秒仅为示例，应按任务调整。',
              'Python 安裝命令為 pip install -U openai；Node.js 為 npm install openai（亦可 bun add openai）。在伺服器環境設定 CODEGO_API_KEY。以下 cURL 使用 Bash 語法，Windows 請用 WSL/Git Bash，或選擇 Python/Node.js。60 秒逾時僅為範例，須按工作調整。',
              'Install with pip install -U openai for Python or npm install openai (also bun add openai) for Node.js. Set CODEGO_API_KEY on your server. cURL examples use Bash; on Windows use WSL/Git Bash or select Python/Node.js. The 60-second SDK timeout is illustrative; adjust it for your task.',
              'Installez pip install -U openai en Python ou npm install openai (ou bun add openai) en Node.js. Définissez CODEGO_API_KEY sur le serveur. Les exemples cURL utilisent Bash : sous Windows, utilisez WSL/Git Bash ou Python/Node.js. Adaptez le délai SDK de 60 secondes à la tâche.',
              'Установите pip install -U openai для Python или npm install openai (также bun add openai) для Node.js. Задайте CODEGO_API_KEY на сервере. Примеры cURL используют Bash: в Windows нужны WSL/Git Bash либо Python/Node.js. Тайм-аут SDK 60 секунд — пример, настройте под задачу.',
              'Pythonは pip install -U openai、Node.jsは npm install openai（または bun add openai）で導入します。サーバーで CODEGO_API_KEY を設定してください。cURL例はBash用です。WindowsではWSL/Git BashかPython/Node.jsを使います。SDKの60秒タイムアウトは例であり、用途に合わせて調整してください。',
              'Cài pip install -U openai cho Python hoặc npm install openai (hay bun add openai) cho Node.js. Đặt CODEGO_API_KEY trên máy chủ. Ví dụ cURL dùng Bash; trên Windows hãy dùng WSL/Git Bash hoặc Python/Node.js. Thời gian chờ SDK 60 giây chỉ là ví dụ, cần chỉnh theo tác vụ.'
            )
          ),
          { kind: 'code', sample: 'streaming' },
          p(
            text(
              'Chat Completions 使用 SSE：累积 choices[].delta.content，工具参数也可能跨帧。choices 为空但含 usage 的事件有效；不能用空 delta 判断结束。原始流以 data: [DONE] 结束。Responses 使用自己的具名事件，不能套用 Chat Completions 解析器。代理必须关闭响应缓冲并保留流式传输。',
              'Chat Completions 使用 SSE：累積 choices[].delta.content，工具參數亦可能跨幀。choices 為空但含 usage 的事件有效；不可用空 delta 判定結束。原始串流以 data: [DONE] 結束。Responses 使用自己的具名事件，不能套用 Chat Completions 解析器。代理須關閉回應緩衝並保留串流。',
              'Chat Completions uses SSE: accumulate choices[].delta.content; tool arguments may also span frames. A usage event with empty choices is valid. An empty delta does not mark completion; the raw stream ends with data: [DONE]. Responses has its own named events and needs a different parser. Disable proxy response buffering and preserve streaming.',
              'Chat Completions utilise SSE : cumulez choices[].delta.content ; les arguments d’outils peuvent aussi être fragmentés. Un événement usage avec choices vide est valide. Un delta vide ne termine pas le flux : attendez data: [DONE]. Responses utilise ses propres événements nommés. Désactivez la mise en tampon du proxy.',
              'Chat Completions использует SSE: накапливайте choices[].delta.content; аргументы инструментов тоже бывают разбиты. Событие usage с пустым choices допустимо. Пустая delta не означает конец: поток заканчивается data: [DONE]. У Responses другие именованные события. Отключите буферизацию ответа в прокси.',
              'Chat CompletionsはSSEです。choices[].delta.content を連結し、分割されるツール引数も集約します。choicesが空でもusageイベントは有効です。空のdeltaでは終了と判断せず、rawストリームの data: [DONE] を待ちます。Responsesは別の名前付きイベントを使います。プロキシの応答バッファを無効にしてください。',
              'Chat Completions dùng SSE: ghép choices[].delta.content; đối số công cụ cũng có thể chia nhiều khung. Sự kiện usage với choices rỗng vẫn hợp lệ. Delta rỗng không phải kết thúc; luồng thô kết thúc bằng data: [DONE]. Responses dùng sự kiện có tên riêng. Tắt bộ đệm phản hồi của proxy.'
            )
          ),
          p(
            text(
              '示例关闭 SDK 自动重试，避免不知情的重复请求。生产中只对可恢复错误做有限退避；已输出内容或调用工具后，先核对请求状态与账单。中断流可能缺少最终 usage，最终费用以使用日志为准。',
              '範例關閉 SDK 自動重試，避免不知情的重複請求。正式環境只對可恢復錯誤做有限退避；已輸出內容或呼叫工具後，先核對請求狀態與賬單。串流中斷可能沒有最終 usage，費用以使用日誌為準。',
              'Examples disable SDK retries to avoid unnoticed duplicate requests. In production, use bounded backoff only for recoverable errors. Check request state and charges after output or tool execution. Interrupted streams may lack final usage; usage logs determine the final charge.',
              'Les exemples désactivent les tentatives automatiques du SDK. En production, limitez les reprises avec temporisation aux erreurs récupérables. Après du contenu ou un outil exécuté, vérifiez état et frais. Un flux interrompu peut manquer d’usage final ; consultez les journaux.',
              'В примерах отключены повторы SDK. В рабочей среде ограничивайте повторы устранимыми ошибками с задержкой. После вывода или выполнения инструмента проверьте состояние и оплату. В прерванном потоке может не быть итогового usage; сверяйтесь с журналом.',
              '気付かない重複要求を防ぐため、例ではSDKの自動再試行を無効にしています。本番では回復可能なエラーだけを回数制限付きで待機・再試行します。出力やツール実行後は状態と料金を確認してください。中断すると最終usageが届かないことがあり、最終料金は使用ログで確認します。',
              'Ví dụ tắt thử lại tự động của SDK để tránh yêu cầu trùng không hay biết. Khi vận hành, chỉ thử lại có giới hạn và giãn cách cho lỗi có thể khắc phục. Sau khi có nội dung hay thực thi công cụ, kiểm tra trạng thái và phí. Luồng bị ngắt có thể thiếu usage cuối; đối chiếu nhật ký.'
            )
          ),
        ],
      },
    ],
  },
  {
    slug: 'tools-structured',
    group: 'api',
    title: text(
      '工具调用与结构化输出',
      '工具呼叫與結構化輸出',
      'Tool calls and structured output',
      'Outils et sorties structurées',
      'Инструменты и структурированный вывод',
      'ツール呼び出しと構造化出力',
      'Gọi công cụ và đầu ra có cấu trúc'
    ),
    summary: text(
      '从声明工具到回传结果，以及 JSON Schema 输出。',
      '從宣告工具到回傳結果，以及 JSON Schema 輸出。',
      'Declare tools, return results and request JSON Schema output.',
      'Déclarer des outils, renvoyer leurs résultats et utiliser JSON Schema.',
      'Объявление инструментов, возврат результатов и JSON Schema.',
      'ツール定義、結果の返送、JSON Schema出力を説明します。',
      'Khai báo công cụ, trả kết quả và yêu cầu đầu ra JSON Schema.'
    ),
    sections: [
      {
        id: 'tool-loop',
        title: text(
          '完成工具调用循环',
          '完成工具呼叫循環',
          'Complete the tool loop',
          'Boucle d’appel des outils',
          'Цикл вызова инструмента',
          'ツールの処理ループ',
          'Hoàn tất vòng gọi công cụ'
        ),
        blocks: [
          p(
            text(
              '先确认模型与渠道支持工具调用。tools 只声明可调用函数，本站不会自动执行你的业务函数。收到 tool_calls 后，由应用校验函数名和参数、检查用户权限、执行工具，再保留完整 assistant 消息并逐一回传 role: "tool" 与对应 tool_call_id。重复调用直到最终文本或达到次数上限。',
              '先確認模型與渠道支援工具呼叫。tools 僅宣告可呼叫函式，本站不會自動執行你的業務函式。收到 tool_calls 後，由應用程式驗證函式名稱與參數、檢查使用者權限、執行工具，再保留完整 assistant 訊息，逐一回傳 role: "tool" 與相應 tool_call_id。重複至最終文字或次數上限。',
              'Confirm tool support for the model and channel. tools declares functions; this site does not execute your business functions. On tool_calls, your application validates function names, arguments and user permissions, executes tools, preserves the complete assistant message and returns each result with role: "tool" and its tool_call_id. Repeat until final text or a loop limit.',
              'Vérifiez la prise en charge des outils. tools déclare des fonctions ; le site n’exécute pas votre logique métier. À réception de tool_calls, votre application valide noms, arguments et droits, exécute les outils, conserve le message assistant complet et renvoie chaque résultat avec role: "tool" et son tool_call_id. Limitez le nombre de cycles.',
              'Проверьте поддержку инструментов моделью и каналом. tools лишь объявляет функции; сайт не исполняет вашу бизнес-логику. Приложение проверяет имена, аргументы и права, выполняет tool_calls, сохраняет полное сообщение assistant и возвращает результаты с role: "tool" и нужным tool_call_id. Ограничьте число циклов.',
              'モデルとチャネルのツール対応を確認します。toolsは関数宣言であり、本サイトが業務関数を自動実行するわけではありません。tool_callsを受けたアプリ側で関数名、引数、利用者の権限を検証し実行します。完全なassistantメッセージを残し、role: "tool" と対応する tool_call_id で各結果を返します。最終テキストか上限回数まで繰り返します。',
              'Xác nhận mô hình và kênh hỗ trợ công cụ. tools chỉ khai báo hàm; trang không tự chạy hàm nghiệp vụ của bạn. Khi nhận tool_calls, ứng dụng kiểm tra tên hàm, đối số và quyền người dùng, thực thi, giữ nguyên thông điệp assistant rồi trả từng kết quả bằng role: "tool" và tool_call_id tương ứng. Lặp đến văn bản cuối hoặc giới hạn vòng.'
            )
          ),
          { kind: 'code', sample: 'tools' },
          p(
            text(
              '下面的 tool-result 只是请求格式演示，call_example 和 12:00 是占位数据。实际程序必须使用上一轮真实返回的调用 ID、assistant 消息和工具结果；有多个调用时为每个 ID 回传一条结果。每一轮模型请求都可能计费。',
              '以下 tool-result 僅示範請求格式，call_example 及 12:00 是佔位資料。實際程式須使用上一輪真實傳回的呼叫 ID、assistant 訊息及工具結果；多個呼叫須各回傳一條結果。每輪模型請求均可能計費。',
              'The tool-result sample illustrates the format only: call_example and 12:00 are placeholders. Use the actual call IDs, assistant message and tool results from the preceding turn. Return one result per ID for multiple calls. Each model round may incur a charge.',
              'L’exemple tool-result illustre uniquement le format : call_example et 12:00 sont fictifs. Utilisez les identifiants, le message assistant et les résultats réels du tour précédent. Renvoyez un résultat par identifiant. Chaque tour peut être facturé.',
              'В tool-result показан только формат: call_example и 12:00 — примеры. Используйте реальные ID, сообщение assistant и результаты предыдущего шага. На каждый ID нужен результат. Каждый запрос модели может оплачиваться.',
              'tool-resultは形式だけの例です。call_exampleと12:00は仮の値なので、前のターンの実際の呼び出しID、assistantメッセージ、ツール結果に置き換えます。複数のIDにはそれぞれ結果を返してください。モデルへの各要求で料金が生じる場合があります。',
              'Mẫu tool-result chỉ minh họa định dạng: call_example và 12:00 là dữ liệu giả. Dùng ID gọi, thông điệp assistant và kết quả thật từ lượt trước. Trả một kết quả cho mỗi ID. Mỗi lượt yêu cầu mô hình có thể tính phí.'
            )
          ),
          { kind: 'code', sample: 'tool-result' },
        ],
      },
      {
        id: 'schema',
        title: text(
          '按 Schema 输出',
          '按 Schema 輸出',
          'Output with a schema',
          'Sortie selon un schéma',
          'Вывод по схеме',
          'スキーマによる出力',
          'Đầu ra theo lược đồ'
        ),
        blocks: [
          p(
            text(
              '仅在支持的模型与渠道上使用 response_format: json_schema。即使 strict=true，应用仍须检查拒答、输出截断和 Schema 校验失败；不得把未校验的模型输出当作 SQL、命令或可信工具参数。仅支持 json_object 的模型不能保证满足某个 Schema。',
              '僅在支援的模型與渠道使用 response_format: json_schema。即使 strict=true，應用程式仍須檢查拒答、輸出截斷及 Schema 驗證失敗；不可將未驗證輸出作為 SQL、指令或可信工具參數。僅支援 json_object 的模型不保證符合特定 Schema。',
              'Use response_format: json_schema only on supported models and channels. Even with strict=true, handle refusals, truncated output and schema-validation failures. Never treat unchecked model output as SQL, shell commands or trusted tool arguments. json_object alone does not guarantee a particular schema.',
              'Utilisez response_format: json_schema uniquement si pris en charge. Même avec strict=true, gérez refus, troncature et échecs de validation. Ne traitez jamais une sortie non validée comme SQL, commande shell ou arguments fiables. json_object seul ne garantit pas un schéma précis.',
              'Используйте response_format: json_schema только при поддержке. Даже с strict=true обрабатывайте отказ, усечение и ошибки проверки схемы. Непроверенный вывод нельзя считать SQL, командой или доверенными аргументами. json_object не гарантирует конкретную схему.',
              '対応モデルとチャネルだけで response_format: json_schema を使います。strict=trueでも拒否、出力切り詰め、スキーマ検証失敗を処理してください。未検証の出力をSQL、シェルコマンド、信頼できるツール引数として扱わないでください。json_objectだけでは特定スキーマに準拠する保証はありません。',
              'Chỉ dùng response_format: json_schema khi mô hình và kênh hỗ trợ. Dù strict=true vẫn phải xử lý từ chối, đầu ra bị cắt và lỗi xác thực. Không dùng đầu ra chưa kiểm tra làm SQL, lệnh shell hay đối số công cụ đáng tin cậy. json_object không bảo đảm lược đồ cụ thể.'
            )
          ),
          { kind: 'code', sample: 'structured' },
        ],
      },
    ],
  },
  {
    slug: 'multimodal',
    group: 'api',
    title: text(
      '图像与多模态接入',
      '圖像與多模態接入',
      'Images and multimodal inputs',
      'Images et entrées multimodales',
      'Изображения и мультимодальность',
      '画像とマルチモーダル入力',
      'Hình ảnh và đầu vào đa phương thức'
    ),
    summary: text(
      '按模型能力选择格式，避免把附件按钮当作已支持。',
      '按模型能力選擇格式，避免將附件按鈕當作已支援。',
      'Choose formats by model capability and verify actual attachment support.',
      'Choisir le format selon le modèle et vérifier les pièces jointes.',
      'Выбирайте формат по возможностям модели и проверяйте вложения.',
      'モデルの能力に応じて形式を選び、添付対応を確認します。',
      'Chọn định dạng theo khả năng mô hình và kiểm tra hỗ trợ tệp.'
    ),
    sections: [
      {
        id: 'image-input',
        title: text(
          '发送图像输入',
          '傳送圖像輸入',
          'Send an image input',
          'Envoyer une image',
          'Передача изображения',
          '画像を送る',
          'Gửi hình ảnh'
        ),
        blocks: [
          p(
            text(
              '图像理解需要支持图像输入的模型与渠道。Chat Completions 可在同一 content 数组中包含 text 和 image_url。把示例地址替换为实际可访问且你有权使用的 HTTPS 图像；数据 URL、大小、分辨率与 MIME 类型限制按具体模型和上游要求。',
              '圖像理解需要支援圖像輸入的模型與渠道。Chat Completions 可在同一 content 陣列包含 text 與 image_url。將範例位址替換為實際可存取且你有權使用的 HTTPS 圖像；資料 URL、大小、解像度及 MIME 類型限制依模型與上游要求。',
              'Image understanding requires a model and channel supporting image input. Chat Completions can mix text and image_url in a content array. Replace the placeholder with an accessible HTTPS image you are authorized to use. Data URLs, size, resolution and MIME limits depend on the model and upstream.',
              'L’analyse d’image exige un modèle et un canal compatibles. Chat Completions accepte text et image_url dans content. Remplacez l’URL fictive par une image HTTPS accessible que vous pouvez utiliser. Les limites de données URL, taille, résolution et MIME dépendent du modèle et du fournisseur.',
              'Нужны модель и канал с поддержкой изображений. В Chat Completions массив content может содержать text и image_url. Замените адрес на доступное HTTPS-изображение, которое вы вправе использовать. Ограничения data URL, размера, разрешения и MIME зависят от модели и поставщика.',
              '画像入力に対応したモデルとチャネルが必要です。Chat Completionsではcontent配列にtextとimage_urlを混在できます。例のURLを、利用権限がありアクセス可能なHTTPS画像に置き換えてください。data URL、容量、解像度、MIME制限はモデルと上流に依存します。',
              'Cần mô hình và kênh hỗ trợ ảnh đầu vào. Chat Completions có thể kết hợp text và image_url trong mảng content. Thay địa chỉ mẫu bằng ảnh HTTPS truy cập được và bạn có quyền dùng. Giới hạn data URL, kích thước, độ phân giải, MIME tùy mô hình và nhà cung cấp.'
            )
          ),
          { kind: 'code', sample: 'vision' },
          p(
            text(
              '图像生成、图像编辑、语音转写、语音合成与视频使用各自接口和计费单位，不能用聊天请求格式代替。请先在模型详情确认输入/输出模态和支持的端点。本站网页对话当前以文本为主，部分附件、摄像头及联网按钮仍提示开发中；API 支持不等于网页已实现对应操作。',
              '圖像生成、編輯、語音轉寫、語音合成與影片使用各自介面及計費單位，不能用聊天格式代替。先在模型詳情確認輸入／輸出模態及支援端點。本站網頁對話目前以文字為主，部分附件、相機及聯網按鈕仍提示開發中；API 支援不等於網頁已實作。',
              'Image generation, editing, transcription, speech synthesis and video use their own endpoints and billing units, not the chat payload. Check input/output modalities and supported endpoints on model details first. Web chat currently focuses on text; some attachment, camera and web buttons still show “in development”. API support does not imply web UI support.',
              'Génération et édition d’images, transcription, synthèse vocale et vidéo ont leurs propres API et unités tarifaires. Vérifiez les modalités et endpoints du modèle. Le chat web se concentre sur le texte ; certains boutons de pièce jointe, caméra et web indiquent encore un développement en cours. Une API disponible ne signifie pas une interface web complète.',
              'Генерация и редактирование изображений, транскрибация, синтез речи и видео имеют отдельные API и единицы оплаты. Сначала проверьте модальности и адреса модели. Веб-чат пока ориентирован на текст; часть кнопок вложений, камеры и веба ещё в разработке. Поддержка API не означает готовность интерфейса.',
              '画像生成・編集、音声文字起こし、音声合成、動画は独自のAPIと課金単位を使います。モデル詳細で入出力モダリティと対応エンドポイントを確認してください。Web対話は現在テキスト中心で、添付・カメラ・Webの一部ボタンは開発中です。API対応はWeb画面の対応を意味しません。',
              'Tạo và sửa ảnh, chuyển âm thanh thành chữ, tổng hợp giọng nói và video có API, đơn vị phí riêng. Kiểm tra phương thức vào/ra và endpoint trong chi tiết mô hình. Trò chuyện web hiện chủ yếu dùng văn bản; một số nút tệp, camera và web vẫn báo đang phát triển. API hỗ trợ không có nghĩa giao diện web đã hỗ trợ.'
            )
          ),
        ],
      },
    ],
  },
  {
    slug: 'web-chat',
    group: 'users',
    title: text(
      '网页对话与历史记录',
      '網頁對話與歷史記錄',
      'Web chat and history',
      'Chat web et historique',
      'Веб-чат и история',
      'Web対話と履歴',
      'Trò chuyện web và lịch sử'
    ),
    summary: text(
      '选择分组和模型，理解费用、重试与本地记录。',
      '選擇分組與模型，了解費用、重試及本機記錄。',
      'Choose a group and model; understand costs, retries and local history.',
      'Choisir un groupe et un modèle ; comprendre frais et historique local.',
      'Выбор группы и модели, стоимость, повторы и локальная история.',
      'グループとモデルを選び、料金・再試行・ローカル履歴を理解します。',
      'Chọn nhóm, mô hình; hiểu phí, thử lại và lịch sử cục bộ.'
    ),
    sections: [
      {
        id: 'start-chat',
        title: text(
          '开始对话',
          '開始對話',
          'Start a conversation',
          'Démarrer une conversation',
          'Начало диалога',
          '対話を始める',
          'Bắt đầu trò chuyện'
        ),
        blocks: [
          p(
            text(
              '从公共导航的“Chat / 对话”或控制台的 Playground 进入，登录后选择有权限的分组和模型。支持连续文本对话、流式输出、停止、编辑、重新生成、复制及删除单条消息；可从模型详情跳转并预选模型。网页对话会产生实际模型调用费用。',
              '從公共導覽的「Chat／對話」或控制台 Playground 進入，登入後選擇有權限的分組及模型。支援連續文字對話、串流輸出、停止、編輯、重新生成、複製及刪除單條訊息；亦可從模型詳情跳轉並預選模型。網頁對話會產生實際模型呼叫費用。',
              'Open Chat in public navigation or Playground in the console, sign in, and choose an authorized group and model. Text conversations support streaming, stop, edit, regenerate, copy and individual-message deletion. Model details can open chat with a model preselected. Web chat makes billable model requests.',
              'Ouvrez Chat dans la navigation publique ou Playground dans la console, connectez-vous et choisissez un groupe et un modèle autorisés. Le texte prend en charge flux, arrêt, modification, régénération, copie et suppression par message. Un modèle peut être présélectionné depuis sa fiche. Les appels sont facturables.',
              'Откройте Chat в навигации или Playground в консоли, войдите и выберите доступную группу и модель. Текстовый диалог поддерживает поток, остановку, редактирование, повторную генерацию, копирование и удаление сообщений. Модель можно выбрать из её карточки. Запросы оплачиваются.',
              '公開ナビゲーションのChatまたはコンソールのPlaygroundを開き、ログイン後に利用可能なグループとモデルを選びます。継続的なテキスト対話、ストリーム、停止、編集、再生成、コピー、個別削除に対応します。モデル詳細からモデルを事前選択できます。Web対話も有料のモデル要求です。',
              'Mở Chat ở điều hướng công khai hoặc Playground trong bảng điều khiển, đăng nhập rồi chọn nhóm và mô hình được phép. Hỗ trợ đối thoại văn bản, truyền luồng, dừng, sửa, tạo lại, sao chép và xóa từng tin. Có thể mở từ chi tiết mô hình với mô hình chọn sẵn. Yêu cầu trò chuyện có tính phí.'
            )
          ),
          {
            kind: 'links',
            items: [
              {
                href: '/playground',
                label: text(
                  '进入对话',
                  '進入對話',
                  'Open chat',
                  'Ouvrir le chat',
                  'Открыть чат',
                  '対話を開く',
                  'Mở trò chuyện'
                ),
              },
            ],
          },
        ],
      },
      {
        id: 'history',
        title: text(
          '记录与费用边界',
          '記錄與費用界線',
          'History and billing boundaries',
          'Historique et limites de facturation',
          'История и оплата',
          '履歴と課金の範囲',
          'Lịch sử và phạm vi tính phí'
        ),
        blocks: [
          p(
            text(
              '当前对话与配置保存在该浏览器的本地存储，不提供账号云端同步或多会话管理。更换浏览器、清除网站数据或使用无痕窗口后不保证保留；共用设备请勿保留敏感内容。删除本地消息不会删除服务器用量日志或上游记录。',
              '目前對話及設定儲存在該瀏覽器的本機儲存空間，未提供帳號雲端同步或多會話管理。更換瀏覽器、清除網站資料或使用無痕視窗後不保證保留；共用裝置請勿保留敏感內容。刪除本機訊息不會刪除伺服器用量日誌或上游記錄。',
              'The current conversation and settings live in this browser’s local storage, without account cloud sync or multiple-conversation management. Changing browsers, clearing site data or using private browsing may lose them. Avoid sensitive content on shared devices. Deleting local messages does not delete server usage logs or upstream records.',
              'La conversation et les réglages sont stockés dans ce navigateur, sans synchronisation cloud ni gestion de plusieurs conversations. Un changement de navigateur, l’effacement des données ou la navigation privée peut les perdre. Évitez les données sensibles sur un appareil partagé. Supprimer un message local ne supprime pas les journaux serveur ou amont.',
              'Диалог и настройки хранятся локально в браузере, без облачной синхронизации или управления несколькими диалогами. Смена браузера, очистка данных или приватный режим могут их удалить. Не оставляйте секреты на общем устройстве. Локальное удаление не удаляет журналы сервера и поставщика.',
              '現在の対話と設定はブラウザーのローカルストレージに保存され、アカウントのクラウド同期や複数対話管理はありません。ブラウザー変更、サイトデータ削除、プライベート利用で失うことがあります。共有端末に機密を残さないでください。ローカル削除はサーバー使用ログや上流の記録を削除しません。',
              'Hội thoại và cấu hình lưu cục bộ trong trình duyệt, chưa có đồng bộ đám mây theo tài khoản hay quản lý nhiều hội thoại. Đổi trình duyệt, xóa dữ liệu hoặc duyệt riêng tư có thể mất dữ liệu. Không giữ nội dung nhạy cảm trên máy dùng chung. Xóa tin cục bộ không xóa nhật ký máy chủ hay bản ghi nhà cung cấp.'
            )
          ),
          p(
            text(
              '重新生成、编辑后重发、重复点击都会产生新的请求。多轮历史会再次发送并计入输入用量；停止生成也可能已消耗 token。模型回答可能错误；工具执行、文件解析和联网是否可用须单独确认，不能从按钮存在推定。',
              '重新生成、編輯後重送及重複點擊均會產生新請求。多輪歷史會再次傳送並計入輸入用量；停止生成亦可能已消耗 token。模型回答可能出錯；工具執行、檔案解析及聯網須另行確認，不能從按鈕存在推定可用。',
              'Regenerating, resending edits and repeated clicks make new requests. Conversation history is sent again and counts toward input usage; stopping may still consume tokens. Model answers may be wrong. Verify tools, file parsing and web access separately rather than inferring support from a button.',
              'Régénérer, renvoyer une modification ou cliquer plusieurs fois crée de nouvelles requêtes. L’historique est renvoyé et compte dans l’entrée ; un arrêt peut déjà avoir consommé des tokens. Les réponses peuvent être fausses. Vérifiez séparément outils, fichiers et accès web.',
              'Повторная генерация, отправка правок и повторные нажатия создают новые запросы. История отправляется снова и входит во входные токены; остановка не исключает расход. Ответы могут быть неверны. Инструменты, файлы и веб требуют отдельной проверки.',
              '再生成、編集後の再送、繰り返しの操作は新しい要求になります。履歴は再送され入力使用量に含まれ、停止時も消費済みトークンが発生します。回答には誤りがあります。ツール、ファイル解析、Web接続は個別に対応を確認してください。',
              'Tạo lại, gửi sau chỉnh sửa và nhấp lặp tạo yêu cầu mới. Lịch sử được gửi lại và tính vào đầu vào; dừng vẫn có thể đã dùng token. Câu trả lời có thể sai. Kiểm tra riêng công cụ, phân tích tệp và truy cập web, không suy ra từ nút bấm.'
            )
          ),
        ],
      },
    ],
  },
  {
    slug: 'model-metrics',
    group: 'market',
    title: text(
      '模型图表与排名口径',
      '模型圖表與排名口徑',
      'Model charts and ranking methodology',
      'Graphiques et méthode de classement',
      'Графики и методика рейтингов',
      'モデル図表とランキングの定義',
      'Biểu đồ và phương pháp xếp hạng'
    ),
    summary: text(
      '理解热度、成功率、首字延迟、吞吐和样本限制。',
      '了解熱度、成功率、首字延遲、吞吐及樣本限制。',
      'Understand popularity, success rates, TTFT, throughput and sample limits.',
      'Comprendre popularité, succès, latence, débit et limites des données.',
      'Популярность, успех, задержка, скорость и ограничения выборки.',
      '人気度、成功率、初回応答、速度、標本の限界を説明します。',
      'Hiểu độ phổ biến, tỷ lệ thành công, độ trễ, thông lượng và giới hạn mẫu.'
    ),
    sections: [
      {
        id: 'definitions',
        title: text(
          '图表能说明什么',
          '圖表能說明甚麼',
          'What charts can tell you',
          'Ce que montrent les graphiques',
          'Что показывают графики',
          '図表から分かること',
          'Biểu đồ cho biết điều gì'
        ),
        blocks: [
          p(
            text(
              '模型广场热度按最近 24 小时请求量排序；市场支持 24 小时、7 天、30 天窗口及多种排序。它们反映本站流量和渠道运行情况，不代表全球使用量或模型能力评测。店铺一至五星来自符合资格用户的评分，也不是模型基准分数。',
              '模型廣場熱度按最近 24 小時請求量排序；市場支援 24 小時、7 日、30 日視窗及多種排序。這些反映本站流量與渠道運作，並非全球用量或模型能力評測。店鋪一至五星來自合資格使用者評分，亦非模型基準分數。',
              'Model-square popularity sorts by requests over the last 24 hours; the market supports 24-hour, 7-day and 30-day windows with several sorts. These describe site traffic and channel operation, not global usage or model intelligence benchmarks. Shop stars are eligible users’ ratings, not benchmark scores.',
              'La popularité des modèles suit les requêtes des dernières 24 heures ; le marché propose 24 heures, 7 jours et 30 jours et plusieurs tris. Ces données décrivent le trafic local et les canaux, pas l’usage mondial ou l’intelligence des modèles. Les étoiles des boutiques proviennent d’utilisateurs éligibles, pas de benchmarks.',
              'Популярность моделей определяется запросами за 24 часа; рынок поддерживает окна 24 часа, 7 и 30 дней и разные сортировки. Это трафик сайта и работа каналов, не мировое использование и не оценка интеллекта. Звёзды магазина — оценки допущенных пользователей, не бенчмарк.',
              'モデル広場の人気順は直近24時間の要求数です。マーケットは24時間・7日・30日の期間と各種並び替えに対応します。本サイトの流量とチャネル運用の指標であり、世界の利用量やモデル能力の評価ではありません。店舗の星も対象利用者による評価で、ベンチマークではありません。',
              'Độ phổ biến ở trang mô hình sắp theo số yêu cầu 24 giờ; chợ hỗ trợ khung 24 giờ, 7 ngày, 30 ngày và nhiều cách sắp xếp. Đây là lưu lượng trang và vận hành kênh, không phải mức dùng toàn cầu hay đánh giá trí tuệ. Sao cửa hàng là người dùng đủ điều kiện chấm, không phải điểm benchmark.'
            )
          ),
          p(
            text(
              'TTFT 表示首个有效输出的等待时间；总延迟包含完整请求耗时；TPS 表示输出速率；成功率描述观测请求结果。模型详情已有延迟与成功率趋势，但图中可用率不能直接解读为 30 天 SLA。比较前核对统计窗口、分组、输入长度、缓存、流式模式与请求样本量；无数据不等于零延迟或 100% 可用。',
              'TTFT 表示首個有效輸出的等待時間；總延遲包含完整請求耗時；TPS 表示輸出速率；成功率描述觀測請求結果。模型詳情已有延遲與成功率趨勢，但圖中可用率不可直接解讀為 30 日 SLA。比較前核對視窗、分組、輸入長度、快取、串流及樣本量；無資料並非零延遲或 100% 可用。',
              'TTFT measures time to first meaningful output; total latency covers the full request; TPS measures output speed; success rate describes observed outcomes. Model details already show latency and success trends, but chart availability is not a 30-day SLA. Compare matching windows, groups, input lengths, caching, streaming modes and sample counts. No data means neither zero latency nor 100% availability.',
              'TTFT mesure l’attente du premier contenu utile ; la latence totale couvre la requête ; TPS mesure le débit ; le taux de succès décrit les résultats observés. Les fiches montrent déjà des tendances, mais la disponibilité du graphique n’est pas un SLA sur 30 jours. Comparez fenêtres, groupes, longueurs d’entrée, cache, flux et effectifs équivalents. Absence de données ne signifie ni zéro latence ni 100 % de disponibilité.',
              'TTFT — ожидание первого полезного вывода; общая задержка — весь запрос; TPS — скорость вывода; успешность — наблюдаемые результаты. Графики модели уже показывают задержки и успех, но это не SLA за 30 дней. Сравнивайте одинаковые окна, группы, длины входа, кэш, потоковый режим и число образцов. Нет данных — не нулевая задержка и не 100% доступность.',
              'TTFTは最初の有効な出力までの時間、総遅延は要求全体、TPSは出力速度、成功率は観測結果です。モデル詳細には遅延と成功率の推移がありますが、図の稼働率は30日SLAを意味しません。期間、グループ、入力長、キャッシュ、ストリーム、標本数を揃えて比較します。データなしを遅延ゼロや稼働率100%と解釈しないでください。',
              'TTFT là thời gian đến đầu ra hữu ích đầu tiên; tổng độ trễ là toàn yêu cầu; TPS là tốc độ đầu ra; tỷ lệ thành công phản ánh kết quả quan sát. Chi tiết mô hình đã có xu hướng độ trễ và thành công nhưng không phải SLA 30 ngày. So sánh cùng khung, nhóm, độ dài đầu vào, bộ nhớ đệm, chế độ luồng và số mẫu. Thiếu dữ liệu không phải độ trễ bằng không hay khả dụng 100%.'
            )
          ),
          p(
            text(
              '当前没有可公开承诺的全球模型榜、应用榜、增长榜、场景能力榜或外部基准分数。选择渠道时同时看价格、稳定性、来源披露与评分，先用自己的任务小额验证。本站排序以页面展示的指标为准；缺少数据时保留空白，不用模拟数据补充图表。',
              '目前沒有可公開承諾的全球模型榜、應用榜、增長榜、場景能力榜或外部基準分數。選擇渠道時綜合價格、穩定性、來源披露及評分，先以自己的工作小額驗證。本站排序以頁面顯示的指標為準；缺少資料時保留空白，不以模擬資料填補圖表。',
              'There is currently no published global model leaderboard, app leaderboard, growth ranking, task-capability ranking or external benchmark score here. Consider prices, reliability, source disclosures and ratings together, then validate using your own tasks. Site sorting follows the metrics shown on the page; leave missing chart data empty rather than supplying synthetic values.',
              'Il n’existe ici aucun classement mondial publié, classement d’applications, de croissance, de compétences par tâche ou score externe. Considérez prix, fiabilité, sources et avis ensemble, puis testez vos propres tâches. Le tri suit les indicateurs affichés sur la page ; laissez les données manquantes vides plutôt que de fournir des valeurs simulées.',
              'Здесь пока нет опубликованных глобальных рейтингов, рейтингов приложений, роста, способностей по задачам или внешних оценок. Учитывайте цену, надёжность, раскрытие источника и отзывы, затем проверяйте своими задачами. Сортировка опирается на показатели страницы; оставляйте отсутствующие данные пустыми, не подставляя вымышленные значения.',
              '現在、世界のモデル順位、アプリ順位、成長順位、タスク能力順位、外部ベンチマーク値は公開していません。価格、安定性、供給元の開示、評価を併せて判断し、自分のタスクで少額検証してください。本サイトの並び替えはページに表示された指標に基づきます。不足データは空欄のままにし、架空値で埋めないでください。',
              'Hiện chưa có bảng toàn cầu, ứng dụng, tăng trưởng, năng lực theo tác vụ hay điểm benchmark ngoài được công bố ở đây. Cân nhắc cùng giá, độ ổn định, nguồn và đánh giá, rồi thử tác vụ của mình với chi phí nhỏ. Thứ tự sắp xếp dựa trên chỉ số hiển thị trên trang; để trống dữ liệu thiếu thay vì thêm giá trị giả.'
            )
          ),
        ],
      },
    ],
  },
  {
    slug: 'support-contact',
    group: 'troubleshooting',
    title: text(
      '社区、支持与问题反馈',
      '社群、支援與問題回報',
      'Community, support and feedback',
      'Communauté, assistance et retours',
      'Сообщество, поддержка и отзывы',
      'コミュニティ・サポート・報告',
      'Cộng đồng, hỗ trợ và phản hồi'
    ),
    summary: text(
      '找到社区入口，提交可排查且不泄露凭据的信息。',
      '尋找社群入口，提交可排查且不洩露憑證的資料。',
      'Find community channels and report actionable issues without exposing credentials.',
      'Trouver les communautés et signaler sans divulguer de secrets.',
      'Каналы связи и полезный отчёт без раскрытия секретов.',
      '秘密を漏らさずに調査可能な報告を送ります。',
      'Tìm kênh cộng đồng và báo lỗi đủ thông tin mà không lộ bí mật.'
    ),
    sections: [
      {
        id: 'channels',
        title: text(
          '联系与反馈',
          '聯絡與回報',
          'Contact and reporting',
          'Contact et signalement',
          'Связь и сообщения',
          '連絡と問題報告',
          'Liên hệ và báo cáo'
        ),
        blocks: [
          {
            kind: 'links',
            items: COMMUNITY_LINKS.map((link) => ({
              href: link.href,
              label:
                link.label === 'Community'
                  ? text(
                      '社区论坛',
                      '社群論壇',
                      'Community forum',
                      'Forum communautaire',
                      'Форум сообщества',
                      'コミュニティフォーラム',
                      'Diễn đàn cộng đồng'
                    )
                  : text(
                      link.label,
                      link.label,
                      link.label,
                      link.label,
                      link.label,
                      link.label,
                      link.label
                    ),
            })),
          },
          p(
            text(
              'X 用于关注动态，Discord 与社区论坛用于交流和问题反馈；这些链接不代表 24 小时人工支持或响应时限承诺。渠道名称、备注、标签或店铺违规请使用市场举报按钮，提交内容由管理员处理。',
              'X 用於追蹤動態，Discord 與社群論壇用於交流及問題回報；這些連結不代表全天候人工支援或回覆時限承諾。渠道名稱、備註、標籤或店鋪違規請使用市場檢舉按鈕，由管理員處理。',
              'Follow updates on X and discuss issues on Discord or the community forum. These links do not imply 24/7 staffed support or a response-time commitment. For abusive channel names, notes, tags or shops, use the market report button for administrator review.',
              'Suivez les actualités sur X et échangez sur Discord ou le forum. Ces liens ne garantissent ni assistance humaine 24 h/24 ni délai de réponse. Signalez les noms, notes, tags ou boutiques abusifs via le bouton du marché pour examen administratif.',
              'Новости — в X, обсуждения и вопросы — в Discord и на форуме. Ссылки не обещают круглосуточную поддержку или срок ответа. Нарушения в названиях, примечаниях, тегах или магазинах отправляйте кнопкой жалобы на рынке администраторам.',
              'Xでは更新情報、Discordとフォーラムでは交流や問題報告ができます。24時間の有人対応や応答期限を保証するものではありません。チャネル名、備考、タグ、店舗の違反はマーケットの通報ボタンで管理者に報告してください。',
              'Theo dõi cập nhật trên X, trao đổi và báo lỗi qua Discord hoặc diễn đàn. Các liên kết không cam kết hỗ trợ người trực 24/7 hay thời hạn phản hồi. Dùng nút báo cáo tại chợ cho tên, ghi chú, thẻ hoặc cửa hàng vi phạm để quản trị xử lý.'
            )
          ),
          p(
            text(
              '反馈调用问题时附上时间与时区、模型 ID、数字渠道 ID（如有）、接口路径、HTTP 状态、错误码和 X-Request-Id，以及脱敏后的最小请求。计费问题补充日志公式和订单号，切勿公开 API Key、Cookie、OAuth Secret、完整付款资料或敏感对话。请先查看状态页与使用日志。',
              '回報呼叫問題時附上時間及時區、模型 ID、數字渠道 ID（如有）、介面路徑、HTTP 狀態、錯誤碼、X-Request-Id，以及去識別化的最小請求。計費問題另附日誌公式及訂單編號；切勿公開 API Key、Cookie、OAuth Secret、完整付款資料或敏感對話。請先查看狀態頁及使用日誌。',
              'Include time and timezone, model ID, numeric channel ID if available, endpoint, HTTP status, error code, X-Request-Id and a redacted minimal request. For billing, add the logged formula and order reference. Never post API keys, cookies, OAuth secrets, full payment details or sensitive conversations. Check status and usage logs first.',
              'Indiquez date et fuseau, modèle, ID numérique du canal si disponible, endpoint, statut HTTP, code d’erreur, X-Request-Id et requête minimale anonymisée. Pour les frais, ajoutez formule et référence de commande. Ne publiez jamais clés, cookies, secrets OAuth, données de paiement complètes ou conversations sensibles. Consultez d’abord statut et journaux.',
              'Укажите время и часовой пояс, ID модели и числовой ID канала при наличии, адрес API, HTTP-статус, код ошибки, X-Request-Id и минимальный обезличенный запрос. Для оплаты приложите формулу и номер заказа. Не публикуйте ключи, cookie, OAuth-секреты, полные платёжные данные или личные диалоги. Сначала проверьте статус и журнал.',
              '時刻とタイムゾーン、モデルID、数字のチャネルID（分かる場合）、APIパス、HTTPステータス、エラーコード、X-Request-Id、秘匿情報を除いた最小要求を添付します。課金はログの式と注文番号も記載します。キー、Cookie、OAuthシークレット、完全な決済情報、機密対話は公開しないでください。先に状態ページと使用ログを確認します。',
              'Kèm thời gian và múi giờ, ID mô hình, ID kênh dạng số nếu có, endpoint, mã HTTP, mã lỗi, X-Request-Id và yêu cầu tối thiểu đã che dữ liệu. Với tính phí, thêm công thức nhật ký và số đơn. Không đăng khóa, cookie, bí mật OAuth, toàn bộ thông tin thanh toán hay hội thoại nhạy cảm. Kiểm tra trạng thái và nhật ký trước.'
            )
          ),
          {
            kind: 'links',
            items: [
              {
                href: '/group-status',
                label: text(
                  '服务状态',
                  '服務狀態',
                  'Service status',
                  'État du service',
                  'Статус сервиса',
                  'サービス状態',
                  'Trạng thái dịch vụ'
                ),
              },
              {
                href: '/privacy-policy',
                label: text(
                  '隐私政策',
                  '私隱政策',
                  'Privacy policy',
                  'Politique de confidentialité',
                  'Политика конфиденциальности',
                  'プライバシーポリシー',
                  'Chính sách quyền riêng tư'
                ),
              },
              {
                href: '/user-agreement',
                label: text(
                  '服务条款与退款规则',
                  '服務條款與退款規則',
                  'Terms and refund rules',
                  'Conditions et remboursements',
                  'Условия и возвраты',
                  '利用規約と返金規定',
                  'Điều khoản và quy định hoàn tiền'
                ),
              },
            ],
          },
        ],
      },
    ],
  },
]
