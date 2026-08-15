import { Link } from '@tanstack/react-router'
import { ArrowRight, Gift, RefreshCcw, Sparkles, Wallet } from 'lucide-react'
import { AnimateInView } from '@/components/animate-in-view'

interface FeaturesProps {
  className?: string
}

const activityCards = [
  {
    icon: <Gift className='size-5 text-amber-600' />,
    tag: '盲盒活动',
    title: '首抽奖励，开盒拿随机奖励',
    description:
      '首次开盒会优先给出更实用的结果，抽到的通用额度和官方 GPT 专属额度都会直接到账。',
    to: '/blind-box',
    cta: '去开盲盒',
  },
  {
    icon: <RefreshCcw className='size-5 text-emerald-600' />,
    tag: '邀请与刷新',
    title: '邀请好友，换额度刷新机会',
    description:
      '被邀请人首购月卡，你就得 1 次订阅额度刷新机会，可清空当前订阅已用额度。',
    to: '/invite-rewards',
    cta: '邀请好友',
  },
  {
    icon: <Wallet className='size-5 text-violet-600' />,
    tag: '额度管理',
    title: '扣费顺序自己说了算',
    description:
      '盲盒奖励、订阅额度和钱包余额共用一套扣费顺序，可随时调整，看清真实可用余额。',
    to: '/wallet',
    cta: '管理钱包',
  },
]

export function Features(_props: FeaturesProps) {
  return (
    <section className='px-6 py-20 md:px-10 md:py-24'>
      <div className='mx-auto max-w-7xl'>
        <AnimateInView className='mx-auto max-w-3xl text-center'>
          <div className='ios-pill text-primary inline-flex items-center gap-2 px-3 py-1 text-xs font-semibold'>
            <Sparkles className='h-3.5 w-3.5' />
            平台福利
          </div>
          <h2 className='text-foreground mt-4 text-[clamp(2rem,4.4vw,3.2rem)] font-semibold tracking-[-0.03em] text-balance'>
            充值之外，还有这些拿额度的方式
          </h2>
          <p className='text-muted-foreground mt-4 text-base leading-7'>
            盲盒、积分、邀请刷新各自独立，又都能换成可用额度。挑一个顺手的开始。
          </p>
        </AnimateInView>

        <div className='mt-12 grid gap-5 md:grid-cols-2'>
          {activityCards.map((card, i) => (
            <AnimateInView key={card.tag} delay={i * 110} className='h-full'>
              <Link
                to={card.to}
                className='ios-floating-shell group flex h-full flex-col p-6 transition-transform duration-200 hover:-translate-y-0.5'
              >
                <div className='text-muted-foreground flex items-center gap-2 text-xs font-semibold tracking-[0.16em]'>
                  {card.icon}
                  {card.tag}
                </div>
                <h3 className='text-foreground mt-3 text-xl font-semibold tracking-[-0.02em]'>
                  {card.title}
                </h3>
                <p className='text-muted-foreground mt-2 flex-1 text-sm leading-6'>
                  {card.description}
                </p>
                <span className='text-primary mt-4 inline-flex items-center gap-1 text-sm font-medium'>
                  {card.cta}
                  <ArrowRight className='size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
                </span>
              </Link>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
