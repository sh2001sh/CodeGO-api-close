import type { DocArticle, DocBlock, Text } from './content'

const text = (
  cn: string,
  hk: string,
  en: string,
  fr: string,
  ru: string,
  ja: string,
  vi: string
): Text => ({ 'zh-CN': cn, 'zh-HK': hk, en, fr, ru, ja, vi })
const paragraph = (copy: Text): DocBlock => ({ kind: 'paragraph', text: copy })

export const environmentSection: DocArticle['sections'][number] = {
  id: 'sdk-environment',
  title: text(
    '配置环境与 SDK',
    '設定環境與 SDK',
    'Environment and SDK setup',
    'Environnement et SDK',
    'Окружение и SDK',
    '環境と SDK の設定',
    'Thiết lập môi trường và SDK'
  ),
  blocks: [
    paragraph(
      text(
        '在自己的服务器或本地开发环境设置 CODEGO_API_KEY。cURL 示例使用环境变量；Python 和 Node.js 使用 openai SDK。密钥只保存在服务端，避免写入浏览器代码、仓库或截图。',
        '在自己的伺服器或本機開發環境設定 CODEGO_API_KEY。cURL 範例使用環境變數；Python 和 Node.js 使用 openai SDK。金鑰只保存在伺服器端，避免寫入瀏覽器程式碼、儲存庫或截圖。',
        'Set CODEGO_API_KEY on your server or local development machine. cURL reads the environment variable; Python and Node.js use the openai SDK. Keep keys server-side and out of browser bundles, repositories and screenshots.',
        'Définissez CODEGO_API_KEY sur votre serveur ou votre poste de développement. cURL lit cette variable ; Python et Node.js utilisent le SDK openai. Gardez les clés côté serveur, hors du navigateur, des dépôts et des captures.',
        'Задайте CODEGO_API_KEY на сервере или локальной машине. cURL читает переменную окружения; Python и Node.js используют SDK openai. Храните ключи на сервере, вне браузерного кода, репозиториев и снимков экрана.',
        'サーバーまたはローカル開発環境に CODEGO_API_KEY を設定します。cURL は環境変数を、Python と Node.js は openai SDK を使用します。キーはサーバー側に保持し、ブラウザーのコード、リポジトリ、スクリーンショットに含めないでください。',
        'Đặt CODEGO_API_KEY trên máy chủ hoặc máy phát triển. cURL đọc biến môi trường; Python và Node.js dùng SDK openai. Giữ khóa ở phía máy chủ, tránh đưa vào mã trình duyệt, kho mã và ảnh chụp.'
      )
    ),
    { kind: 'code', sample: 'environment' },
    paragraph(
      text(
        'SDK 的 Base URL 使用本站地址加 /v1；只追加一次 /v1。下方示例随当前站点生成地址，可复制后核对域名。公开模型建议用于填写示例，实际访问权限以携带密钥查询 GET /v1/models 的结果为准。',
        'SDK 的 Base URL 使用本站位址加 /v1；只附加一次 /v1。下方範例按目前網站產生位址，複製後請核對網域。公開模型建議用於填寫範例，實際存取權以攜帶金鑰查詢 GET /v1/models 的結果為準。',
        'Use this site’s origin followed by /v1 as the SDK base URL, appending /v1 only once. Verify the generated domain after copying. Public model suggestions help fill examples; GET /v1/models with your key determines your actual model access.',
        'Utilisez l’origine du site suivie de /v1 comme URL du SDK, une seule fois. Vérifiez le domaine généré après copie. Les suggestions publiques servent aux exemples ; GET /v1/models avec votre clé détermine votre accès réel.',
        'Base URL SDK — адрес этого сайта с одним суффиксом /v1. После копирования проверьте домен. Публичные подсказки моделей предназначены для примеров; реальный доступ проверяйте через GET /v1/models со своим ключом.',
        'SDK の Base URL は本サイトのオリジンに /v1 を一度だけ付けた値です。コピー後にドメインを確認してください。公開モデル候補は例の入力用です。実際の利用権限はキー付きの GET /v1/models で確認します。',
        'Base URL của SDK là địa chỉ gốc của trang cộng /v1, chỉ thêm một lần. Kiểm tra tên miền sau khi sao chép. Gợi ý mô hình công khai giúp điền ví dụ; dùng GET /v1/models kèm khóa để xác nhận quyền truy cập thực tế.'
      )
    ),
  ],
}

export const productionArticles: readonly DocArticle[] = [
  {
    slug: 'responses-streaming',
    group: 'api',
    title: text(
      'Responses 流式与事件',
      'Responses 串流與事件',
      'Responses streaming and events',
      'Flux et événements Responses',
      'Потоки и события Responses',
      'Responses のストリーミングとイベント',
      'Luồng và sự kiện Responses'
    ),
    summary: text(
      '识别文本增量、终止状态与用量。',
      '辨識文字增量、終止狀態與用量。',
      'Handle text deltas, terminal states and usage.',
      'Traiter les deltas, états finaux et usages.',
      'Обработка текста, завершения и использования.',
      'テキスト差分、終了状態、使用量の処理。',
      'Xử lý văn bản tăng dần, trạng thái cuối và lượng dùng.'
    ),
    sections: [
      {
        id: 'request',
        title: text(
          '发送流式请求',
          '傳送串流請求',
          'Send a streaming request',
          'Envoyer une requête en flux',
          'Отправка потокового запроса',
          'ストリーミング要求の送信',
          'Gửi yêu cầu truyền luồng'
        ),
        blocks: [
          paragraph(
            text(
              'POST /v1/responses 使用 model、input 和 stream: true。先确认所选模型与分组支持 Responses；Chat Completions 的 messages、choices 和结束标记不能直接套用到此协议。示例关闭 SDK 自动重试，便于明确处理断流与费用。',
              'POST /v1/responses 使用 model、input 和 stream: true。先確認所選模型與分組支援 Responses；Chat Completions 的 messages、choices 及結束標記不能直接套用。範例關閉 SDK 自動重試，方便明確處理斷流與費用。',
              'POST /v1/responses uses model, input and stream: true. Confirm Responses support for your model and group. Chat Completions messages, choices and completion markers do not apply directly. Samples disable SDK retries so interruptions and charges are handled explicitly.',
              'POST /v1/responses utilise model, input et stream: true. Vérifiez la prise en charge du modèle et du groupe. Les messages, choices et marqueurs de fin de Chat Completions ne se transposent pas directement. Les exemples désactivent les tentatives automatiques du SDK.',
              'POST /v1/responses использует model, input и stream: true. Убедитесь в поддержке моделью и группой. messages, choices и маркеры завершения Chat Completions напрямую неприменимы. Автоповторы SDK отключены для явной обработки обрывов и списаний.',
              'POST /v1/responses では model、input、stream: true を指定します。モデルとグループの Responses 対応を確認してください。Chat Completions の messages、choices、終了マーカーはそのまま使えません。例では切断と課金を明示的に扱うため SDK の自動再試行を無効にしています。',
              'POST /v1/responses dùng model, input và stream: true. Xác nhận mô hình và nhóm hỗ trợ Responses. Không dùng trực tiếp messages, choices hay dấu kết thúc của Chat Completions. Ví dụ tắt tự thử lại của SDK để xử lý rõ việc đứt luồng và tính phí.'
            )
          ),
          { kind: 'code', sample: 'responses-streaming' },
        ],
      },
      {
        id: 'lifecycle',
        title: text(
          '识别结束与失败',
          '辨識結束與失敗',
          'Recognize completion and failure',
          'Reconnaître la fin et les échecs',
          'Завершение и ошибки',
          '完了と失敗の判定',
          'Nhận biết hoàn tất và lỗi'
        ),
        blocks: [
          {
            kind: 'table',
            headers: [
              text(
                '事件',
                '事件',
                'Event',
                'Événement',
                'Событие',
                'イベント',
                'Sự kiện'
              ),
              text(
                '应用处理',
                '應用程式處理',
                'Application handling',
                'Traitement',
                'Действие приложения',
                'アプリの処理',
                'Cách xử lý'
              ),
            ],
            rows: [
              [
                text(
                  'response.output_text.delta',
                  'response.output_text.delta',
                  'response.output_text.delta',
                  'response.output_text.delta',
                  'response.output_text.delta',
                  'response.output_text.delta',
                  'response.output_text.delta'
                ),
                text(
                  '追加 delta 文本，保留事件顺序。',
                  '附加 delta 文字，保留事件順序。',
                  'Append delta text in event order.',
                  'Ajouter le texte delta dans l’ordre.',
                  'Добавляйте delta в порядке событий.',
                  'イベント順に delta を追加します。',
                  'Nối văn bản delta theo thứ tự sự kiện.'
                ),
              ],
              [
                text(
                  'response.completed',
                  'response.completed',
                  'response.completed',
                  'response.completed',
                  'response.completed',
                  'response.completed',
                  'response.completed'
                ),
                text(
                  '记录最终 response 和 usage，再标记完成。',
                  '記錄最終 response 與 usage，再標記完成。',
                  'Record the final response and usage, then mark complete.',
                  'Enregistrer response et usage avant de terminer.',
                  'Сохраните итоговые response и usage, затем завершите.',
                  '最終 response と usage を記録して完了とします。',
                  'Lưu response và usage cuối rồi đánh dấu hoàn tất.'
                ),
              ],
              [
                text(
                  'response.failed / response.incomplete / error',
                  'response.failed / response.incomplete / error',
                  'response.failed / response.incomplete / error',
                  'response.failed / response.incomplete / error',
                  'response.failed / response.incomplete / error',
                  'response.failed / response.incomplete / error',
                  'response.failed / response.incomplete / error'
                ),
                text(
                  '保留部分输出与错误；流意外结束同样按未完成处理。',
                  '保留部分輸出與錯誤；串流意外結束同樣按未完成處理。',
                  'Keep partial output and the error; unexpected stream closure is also incomplete.',
                  'Conserver la sortie partielle et l’erreur ; une fermeture inattendue reste incomplète.',
                  'Сохраните частичный вывод и ошибку; неожиданный обрыв также означает незавершённость.',
                  '部分出力とエラーを保持します。予期しない終了も未完了として扱います。',
                  'Giữ kết quả một phần và lỗi; luồng đóng bất ngờ cũng là chưa hoàn tất.'
                ),
              ],
            ],
          },
          paragraph(
            text(
              '此文本示例只消费文本增量；工具参数、文件、拒答与其他输出项需按事件类型分别处理。不要仅凭 HTTP 200 或一段文字判定整次请求成功。',
              '此文字範例只處理文字增量；工具參數、檔案、拒答及其他輸出項須按事件類型分別處理。請勿僅憑 HTTP 200 或一段文字判定整次請求成功。',
              'This text example consumes text deltas only. Handle tool arguments, files, refusals and other output items by event type. HTTP 200 or a text fragment alone does not establish successful completion.',
              'Cet exemple traite seulement le texte. Gérez séparément les arguments d’outils, fichiers, refus et autres sorties selon leur type. Un HTTP 200 ou un fragment de texte ne prouve pas la réussite complète.',
              'Пример обрабатывает только текстовые изменения. Аргументы инструментов, файлы, отказы и другие элементы разбирайте по типу события. HTTP 200 или фрагмент текста не подтверждает полное завершение.',
              'この例はテキスト差分のみを処理します。ツール引数、ファイル、拒否、その他の出力はイベント種別ごとに処理してください。HTTP 200 や一部の文章だけでは全体の成功を判定できません。',
              'Ví dụ này chỉ xử lý văn bản tăng dần. Xử lý tham số công cụ, tệp, từ chối và đầu ra khác theo loại sự kiện. HTTP 200 hay một đoạn văn bản chưa đủ xác nhận toàn bộ yêu cầu thành công.'
            )
          ),
        ],
      },
      {
        id: 'disconnect',
        title: text(
          '断开、取消与结算',
          '斷線、取消與結算',
          'Disconnects, cancellation and settlement',
          'Déconnexion, annulation et facturation',
          'Обрыв, отмена и расчёты',
          '切断、キャンセル、精算',
          'Ngắt kết nối, hủy và quyết toán'
        ),
        blocks: [
          paragraph(
            text(
              '取消时停止读取并关闭 SDK 流。连接关闭与上游停止处理之间可能存在时间差，已经产生的用量仍需核对日志。不要自动重放已有输出的请求。后台任务和 WebSocket 的生命周期另有接口及能力条件，不能从此 HTTP 示例推断全部可用。',
              '取消時停止讀取並關閉 SDK 串流。連線關閉與上游停止處理可能有時間差，已產生的用量仍須核對日誌。請勿自動重播已有輸出的請求。背景任務和 WebSocket 另有介面及能力條件，不能由此 HTTP 範例推斷全部可用。',
              'On cancellation, stop consuming and close the SDK stream. Connection closure and upstream cancellation can differ in time; reconcile generated usage in logs. Do not automatically replay requests that already produced output. Background jobs and WebSocket lifecycles have separate endpoints and capability requirements.',
              'Pour annuler, arrêtez la lecture et fermez le flux SDK. La fermeture et l’arrêt amont peuvent être décalés ; rapprochez l’usage dans les journaux. Ne rejouez pas automatiquement une requête ayant déjà produit une sortie. Les tâches en arrière-plan et WebSocket ont leurs propres conditions.',
              'При отмене прекратите чтение и закройте поток SDK. Закрытие соединения и остановка поставщика могут произойти в разное время; проверьте использование в журнале. Не повторяйте автоматически запрос с частичным выводом. Фоновые задачи и WebSocket имеют отдельные интерфейсы и требования.',
              'キャンセル時は読み取りを止めて SDK ストリームを閉じます。接続終了と上流処理の停止には時間差があり得るため、発生した使用量をログで確認します。出力済みの要求を自動で再送しないでください。バックグラウンド処理と WebSocket は別の API と対応条件があります。',
              'Khi hủy, dừng đọc và đóng luồng SDK. Việc đóng kết nối và dừng xử lý ở nhà cung cấp có thể lệch thời điểm; đối chiếu lượng dùng trong nhật ký. Tránh tự phát lại yêu cầu đã có đầu ra. Tác vụ nền và WebSocket có API và điều kiện hỗ trợ riêng.'
            )
          ),
        ],
      },
    ],
  },
  {
    slug: 'concurrency',
    group: 'troubleshooting',
    title: text(
      '并发、排队与重试',
      '並行、排隊與重試',
      'Concurrency, queues and retries',
      'Concurrence, files et nouvelles tentatives',
      'Параллелизм, очереди и повторы',
      '同時実行、キュー、再試行',
      'Đồng thời, hàng đợi và thử lại'
    ),
    summary: text(
      '按实际工作负载控制请求规模与失败恢复。',
      '按實際工作負載控制請求規模與失敗恢復。',
      'Bound request load and recovery for your workload.',
      'Encadrer la charge et la reprise selon vos usages.',
      'Ограничение нагрузки и восстановление запросов.',
      '実際の負荷に応じた要求数と復旧の制御。',
      'Giới hạn tải và phục hồi theo tác vụ thực tế.'
    ),
    sections: [
      {
        id: 'limits',
        title: text(
          '区分速率与并发',
          '區分速率與並行',
          'Rate and concurrency are separate',
          'Distinguer débit et concurrence',
          'Частота и параллелизм',
          'レートと同時実行を分ける',
          'Phân biệt tốc độ và đồng thời'
        ),
        blocks: [
          paragraph(
            text(
              'RPM 是时间窗内的请求数，TPM 是 Token 用量，并发是同时进行中的请求数。长上下文、长输出与流式连接会增加占用时间。账号、密钥、分组、渠道及上游可分别施加限制，具体可用额度以当前配置和响应为准。本站不对所有模型给出统一固定并发值。',
              'RPM 是時間窗內的請求數，TPM 是 Token 用量，並行數是同時進行中的請求數。長上下文、長輸出及串流連線會增加佔用時間。帳戶、金鑰、分組、渠道及上游可分別施加限制，實際額度以目前設定及回應為準。本站不對所有模型提供統一固定並行值。',
              'RPM counts requests in a time window; TPM counts tokens; concurrency counts in-flight requests. Long contexts, outputs and streams extend occupancy. Accounts, keys, groups, channels and upstreams can impose separate limits. Check current configuration and responses; there is no universal fixed concurrency value for every model.',
              'RPM compte les requêtes par période, TPM les tokens et la concurrence les requêtes en cours. Les longs contextes, sorties et flux prolongent leur occupation. Comptes, clés, groupes, canaux et fournisseurs peuvent limiter séparément. Consultez la configuration et les réponses actuelles ; aucune valeur unique ne couvre tous les modèles.',
              'RPM — запросы за период, TPM — токены, параллелизм — одновременные запросы. Длинный контекст, вывод и потоки увеличивают время обработки. Аккаунт, ключ, группа, канал и поставщик могут ограничивать нагрузку отдельно. Ориентируйтесь на настройки и ответы; общего фиксированного лимита для всех моделей нет.',
              'RPM は時間枠内の要求数、TPM はトークン数、同時実行数は処理中の要求数です。長い文脈、出力、ストリームは占有時間を延ばします。アカウント、キー、グループ、チャネル、上流で個別の制限が適用され得ます。現在の設定と応答を確認し、全モデル共通の固定値を想定しないでください。',
              'RPM đếm yêu cầu theo khoảng thời gian, TPM đếm token, còn mức đồng thời đếm yêu cầu đang xử lý. Ngữ cảnh dài, đầu ra dài và luồng kéo dài thời gian chiếm dụng. Tài khoản, khóa, nhóm, kênh và nhà cung cấp có thể đặt giới hạn riêng. Xem cấu hình và phản hồi hiện tại; không có một mức cố định chung cho mọi mô hình.'
            )
          ),
        ],
      },
      {
        id: 'queue',
        title: text(
          '使用有界队列',
          '使用有界佇列',
          'Use a bounded queue',
          'Utiliser une file bornée',
          'Ограниченная очередь',
          '上限のあるキューを使う',
          'Dùng hàng đợi có giới hạn'
        ),
        blocks: [
          paragraph(
            text(
              '在应用端使用信号量（Semaphore）限制进行中请求数，并设置排队容量和总截止时间。队列已满时返回明确的忙碌状态；取消或超时的任务应离开队列，完成或失败后在 finally 中释放槽位。多个服务实例需要共享预算，避免每个实例独立放大同一密钥的负载。',
              '在應用程式端使用訊號量（Semaphore）限制進行中請求數，並設定排隊容量及總截止時間。佇列已滿時回傳明確的忙碌狀態；取消或逾時的任務須移出佇列，完成或失敗後在 finally 釋放名額。多個服務執行個體需要共用預算，避免各自放大同一金鑰的負載。',
              'Use a semaphore to cap in-flight requests, with a queue capacity and overall deadline in your application. Reject overflow explicitly; remove cancelled or expired jobs and release slots in finally on success or failure. Share the budget across service instances so each instance does not multiply load for the same key.',
              'Utilisez un sémaphore pour plafonner les requêtes actives, avec une capacité de file et une échéance totale. Signalez explicitement la saturation, retirez les tâches annulées ou expirées et libérez les places dans finally. Partagez le budget entre instances pour éviter de multiplier la charge d’une même clé.',
              'Ограничьте активные запросы семафором, задайте размер очереди и общий срок. Явно отклоняйте переполнение, удаляйте отменённые и просроченные задачи, освобождайте места в finally при любом результате. Делите бюджет между экземплярами сервиса, чтобы не умножать нагрузку одного ключа.',
              'アプリ側でセマフォによる実行中の上限、キュー容量、全体の期限を設定します。満杯なら混雑状態を明示し、キャンセル済みや期限切れのジョブを除去します。成功時も失敗時も finally で枠を解放します。複数インスタンスでは予算を共有し、同じキーの負荷が増幅しないようにします。',
              'Dùng semaphore để giới hạn yêu cầu đang chạy, đặt sức chứa hàng đợi và thời hạn tổng ở ứng dụng. Báo rõ khi đầy; loại tác vụ đã hủy hoặc hết hạn, giải phóng chỗ trong finally dù thành công hay lỗi. Chia sẻ ngân sách giữa các bản sao dịch vụ để tránh nhân tải cho cùng một khóa.'
            )
          ),
        ],
      },
      {
        id: 'backoff',
        title: text(
          '限制重试预算',
          '限制重試預算',
          'Bound the retry budget',
          'Limiter le budget de reprise',
          'Бюджет повторных попыток',
          '再試行の予算を制限する',
          'Giới hạn ngân sách thử lại'
        ),
        blocks: [
          paragraph(
            text(
              '只对可恢复且确认可安全重复执行的短暂失败重试，并同时限制次数与总耗时。收到 429 时降低并发；存在 Retry-After 时按秒数或 HTTP 日期解析并等待。其余暂时失败使用带随机抖动的指数退避。SDK、网关及业务队列的重试可能叠加，配置时核对总尝试数。',
              '只對可恢復且確認可安全重複執行的暫時失敗重試，並同時限制次數與總耗時。收到 429 時降低並行數；存在 Retry-After 時按秒數或 HTTP 日期解析並等待。其他暫時失敗使用帶隨機抖動的指數退避。SDK、閘道及業務佇列的重試可能疊加，設定時核對總嘗試數。',
              'Retry recoverable transient failures only when repeating the operation is safe, with both an attempt cap and total deadline. Reduce concurrency on 429; honor Retry-After as seconds or an HTTP date when provided. Otherwise use exponential backoff with jitter. SDK, gateway and job retries can compound; check the total attempt budget.',
              'Réessayez uniquement les échecs transitoires récupérables lorsque répéter l’opération est sûr, avec plafond d’essais et échéance totale. Réduisez la concurrence sur 429 et respectez Retry-After en secondes ou date HTTP. Sinon, appliquez un délai exponentiel aléatoire. Les reprises du SDK, de la passerelle et des tâches peuvent se cumuler.',
              'Повторяйте только устранимые временные ошибки, если повтор операции безопасен, с пределом попыток и общего времени. При 429 снижайте параллелизм; учитывайте Retry-After в секундах или формате даты HTTP. Иначе используйте экспоненциальную задержку со случайным разбросом. Повторы SDK, шлюза и очереди могут перемножаться.',
              '回復可能で安全に繰り返せる一時エラーだけを、回数と全体期限を設けて再試行します。429 では同時実行を減らし、Retry-After があれば秒数または HTTP 日付として扱います。それ以外はジッター付き指数バックオフを使います。SDK、ゲートウェイ、ジョブの再試行が重なるため、合計試行数を確認してください。',
              'Chỉ thử lại lỗi tạm thời có thể phục hồi khi lặp thao tác là an toàn, giới hạn cả số lần và thời gian tổng. Giảm đồng thời khi gặp 429; đọc Retry-After dạng số giây hoặc ngày HTTP nếu có. Nếu không, dùng lùi theo cấp số nhân kèm ngẫu nhiên. Lần thử của SDK, cổng và tác vụ có thể cộng dồn; kiểm tra tổng ngân sách.'
            )
          ),
          paragraph(
            text(
              '401、403 与参数错误先修正配置；已输出、工具已执行或结算结果未知时，不自动重放整个请求。重试本身也占用并发和费用预算。应用对外部写入维护自己的业务幂等键，不依赖所有模型接口具备通用幂等保证。',
              '401、403 及參數錯誤先修正設定；已有輸出、工具已執行或結算結果未知時，請勿自動重播整個請求。重試亦佔用並行與費用預算。應用程式須為外部寫入維護業務冪等鍵，避免依賴所有模型介面都有通用冪等保證。',
              'Correct 401, 403 and invalid parameters first. Do not automatically replay a request after output, tool execution or an uncertain settlement. Retries consume concurrency and cost budgets too. Use application-level idempotency for external writes rather than assuming every model endpoint guarantees it.',
              'Corrigez d’abord les 401, 403 et paramètres invalides. Ne rejouez pas automatiquement après une sortie, un outil exécuté ou une facturation incertaine. Les reprises consomment aussi charge et budget. Protégez les écritures externes par votre propre idempotence métier.',
              'Сначала исправьте 401, 403 и неверные параметры. Не повторяйте автоматически запрос после вывода, выполнения инструмента или неопределённого расчёта. Повторы также расходуют лимиты и средства. Для внешних записей используйте собственные ключи идемпотентности.',
              '401、403、パラメーターの誤りは先に修正します。出力済み、ツール実行済み、精算結果が不明な要求は自動再送しないでください。再試行も同時実行枠と費用を消費します。外部書き込みの冪等性はアプリ側の業務キーで管理します。',
              'Sửa 401, 403 và tham số sai trước. Tránh tự phát lại sau khi có đầu ra, công cụ đã chạy hoặc quyết toán chưa rõ. Thử lại cũng chiếm mức đồng thời và ngân sách phí. Dùng khóa chống trùng ở cấp ứng dụng cho các thao tác ghi bên ngoài.'
            )
          ),
        ],
      },
      {
        id: 'measure',
        title: text(
          '按真实负载验收',
          '按真實負載驗收',
          'Validate with representative load',
          'Valider avec une charge représentative',
          'Проверка на реальной нагрузке',
          '実際の負荷で検証する',
          'Kiểm chứng bằng tải đại diện'
        ),
        blocks: [
          paragraph(
            text(
              '从低并发逐步增加，分别记录排队时间、首 Token 时间、总耗时、P50/P95/P99、错误与 Token 吞吐。测试短请求、长上下文、长流、取消与上游失败，使用独立测试密钥及成本上限。小请求的结果不能直接代表长流容量；页面读取测试也不能证明模型推理容量。',
              '由低並行逐步增加，分別記錄排隊時間、首 Token 時間、總耗時、P50/P95/P99、錯誤與 Token 吞吐。測試短請求、長上下文、長串流、取消及上游失敗，使用獨立測試金鑰與成本上限。短請求結果不能直接代表長串流容量；頁面讀取測試亦不能證明模型推理容量。',
              'Ramp up gradually from low concurrency and record queue time, time to first token, total duration, P50/P95/P99, errors and token throughput. Cover short requests, long contexts, long streams, cancellation and upstream failure with a separate test key and cost cap. Short-request results do not establish long-stream capacity; page-read tests do not establish inference capacity.',
              'Augmentez progressivement la charge et mesurez attente, premier token, durée totale, P50/P95/P99, erreurs et débit de tokens. Couvrez requêtes courtes, longs contextes, flux longs, annulations et échecs amont avec une clé et un budget dédiés. Les tests courts ne prouvent pas la capacité des flux longs ; les lectures de pages ne prouvent pas celle de l’inférence.',
              'Повышайте параллелизм постепенно, измеряя очередь, первый токен, полное время, P50/P95/P99, ошибки и токены в секунду. Проверяйте короткие запросы, длинный контекст, потоки, отмену и сбои поставщика с отдельным ключом и лимитом затрат. Короткие запросы не доказывают ёмкость длинных потоков; чтение страниц не проверяет ёмкость инференса.',
              '低い同時実行数から徐々に増やし、キュー待ち、初回トークン時間、総時間、P50/P95/P99、エラー、トークンスループットを記録します。専用キーと費用上限で短い要求、長文脈、長時間ストリーム、キャンセル、上流障害を検証します。短い要求の結果は長いストリームの容量を、ページ読み取り試験は推論容量を証明しません。',
              'Tăng dần từ mức đồng thời thấp, ghi thời gian chờ, token đầu, tổng thời gian, P50/P95/P99, lỗi và thông lượng token. Kiểm tra yêu cầu ngắn, ngữ cảnh dài, luồng dài, hủy và lỗi nhà cung cấp bằng khóa riêng cùng trần chi phí. Kết quả yêu cầu ngắn không chứng minh sức chứa luồng dài; kiểm tra đọc trang không chứng minh năng lực suy luận.'
            )
          ),
          {
            kind: 'links',
            items: [
              {
                href: '/docs?article=errors',
                label: text(
                  '错误与重试',
                  '錯誤與重試',
                  'Errors and retries',
                  'Erreurs et reprises',
                  'Ошибки и повторы',
                  'エラーと再試行',
                  'Lỗi và thử lại'
                ),
              },
              {
                href: '/docs?article=billing',
                label: text(
                  '用量与结算',
                  '用量與結算',
                  'Usage and settlement',
                  'Usage et facturation',
                  'Использование и расчёты',
                  '使用量と精算',
                  'Lượng dùng và quyết toán'
                ),
              },
              {
                href: '/usage-logs',
                label: text(
                  '使用日志',
                  '使用日誌',
                  'Usage logs',
                  'Journaux d’usage',
                  'Журнал использования',
                  '使用ログ',
                  'Nhật ký sử dụng'
                ),
              },
            ],
          },
        ],
      },
    ],
  },
]
