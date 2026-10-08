import { Link } from '@tanstack/react-router'
import { ChevronDown } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'

const questions = [
  {
    id: 'pricing',
    question: '在哪里查看模型价格？',
    answer:
      '模型页提供模型定价；选择分组时还需核对倍率。实际费用由模型计价规则、用量和所选分组共同决定。',
    to: '/models',
    label: '查看模型与价格',
  },
  {
    id: 'groups',
    question: '同一个模型，为什么有不同分组？',
    answer:
      '分组对应不同的上游服务和访问条件。比较价格时，也请查看模型验证结果、最近探测时间与延迟；单次探测不代表持续可用性保证。',
    to: '/docs',
    hash: 'groups',
    label: '了解分组选择',
  },
  {
    id: 'plans',
    question: '余额、新套餐和旧套餐有什么区别？',
    answer:
      '余额按调用费用扣除；新套餐按购买时的额度、有效期和适用分组使用，不再采用旧版十倍扣费。旧套餐可按原规则继续使用，符合条件且未到期的旧套餐可在钱包查看转余额报价。转换后对应权益不能再刷新。',
    to: '/docs',
    hash: 'plans',
    label: '查看套餐与余额规则',
  },
  {
    id: 'failures',
    question: '调用失败或中断，会扣费吗？',
    answer:
      '输出前失败不计费；已经产生输出后中断，可能按已产生的用量结算。请用请求编号在使用日志中核对最终状态、用量和费用。',
    to: '/docs',
    hash: 'errors',
    label: '查看错误处理',
  },
  {
    id: 'invoices',
    question: '购买后如何下载香港商业发票？',
    answer:
      '在账单明细的已支付订单中填写个人姓名或公司全称及购买方地址，即可自助开具 PDF。首次开具后抬头与地址固定，后续直接下载；退款中或已退款的订单不可开具或下载。',
    to: '/billing',
    hash: 'invoices',
    label: '前往账单明细',
  },
  {
    id: 'refunds',
    question: '未使用的额度可以退款吗？',
    answer:
      '符合条件的已支付订单可在钱包查看可退额度、费用和预计退款金额。已消耗额度不退款，赠送额度不能直接提现；支付渠道和订单状态也会影响退款资格。',
    to: '/refund-policy',
    label: '查看退款规则',
  },
  {
    id: 'compatibility',
    question: '已有 OpenAI SDK 或客户端如何接入？',
    answer:
      '将支持自定义接口地址的客户端配置为本站的 /v1 地址，并使用 CodeGo API Key。模型名以模型页为准；工具调用、图像和其他能力取决于所选模型与分组。',
    to: '/docs',
    hash: 'clients',
    label: '查看接入步骤',
  },
  {
    id: 'support',
    question: '遇到问题，应该提供哪些信息？',
    answer:
      '请提供发生时间、模型、分组、请求编号和错误信息。支付问题可补充订单编号。不要公开 API Key、密码、验证码或未经脱敏的对话内容。',
    to: '/support',
    label: '联系支持',
  },
] as const

/** Native disclosure keeps questions readable and keyboard accessible without another dependency. */
export function FAQ(props: { limit?: number }) {
  const { t } = useTranslation()
  return (
    <div className="public-faq">
      {questions.slice(0, props.limit ?? questions.length).map((item) => (
        <details key={item.id} id={item.id}>
          <summary>
            {t(item.question)}
            <ChevronDown size={18} aria-hidden />
          </summary>
          <div className="public-faq-answer">
            <p>{t(item.answer)}</p>
            <Link
              to={item.to}
              hash={'hash' in item ? item.hash : undefined}
              className="home-text-link"
            >
              {t(item.label)}
            </Link>
          </div>
        </details>
      ))}
    </div>
  )
}
