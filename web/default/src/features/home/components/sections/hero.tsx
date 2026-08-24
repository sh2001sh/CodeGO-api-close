import type { ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import {
  ArrowRight,
  CreditCard,
  Gift,
  Layers,
  MousePointerClick,
  Wallet,
} from 'lucide-react'
import { motion, useReducedMotion, type Variants } from 'motion/react'
import { getLobeIcon } from '@/lib/lobe-icon'
import { Button } from '@/components/ui/button'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

const heroStagger: Variants = {
  initial: {},
  animate: { transition: { staggerChildren: 0.08, delayChildren: 0.08 } },
}

const heroItem: Variants = {
  initial: { opacity: 0, y: 18, filter: 'blur(6px)' },
  animate: {
    opacity: 1,
    y: 0,
    filter: 'blur(0px)',
    transition: { duration: 0.55, ease: EASE_OUT_QUINT },
  },
}

const heroShell: Variants = {
  initial: { opacity: 0, y: 24, scale: 0.98 },
  animate: {
    opacity: 1,
    y: 0,
    scale: 1,
    transition: { duration: 0.6, ease: EASE_OUT_QUINT, delay: 0.18 },
  },
}

const shellList: Variants = {
  initial: {},
  animate: { transition: { staggerChildren: 0.07, delayChildren: 0.42 } },
}

const shellItem: Variants = {
  initial: { opacity: 0, y: 10 },
  animate: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.4, ease: EASE_OUT_QUINT },
  },
}

const models = [
  { key: 'OpenAI', label: 'OpenAI' },
  { key: 'Claude.Color', label: 'Claude' },
]

const advantages = [
  {
    icon: <CreditCard className='h-4 w-4 text-emerald-600' />,
    title: '价格更耐用',
    description: '人民币付费，按美元信用额度计费，长期调用更划算。',
  },
  {
    icon: <Gift className='h-4 w-4 text-amber-600' />,
    title: '活动福利多',
    description: '盲盒首抽奖励、邀请刷新、额度加成，多重福利叠加。',
  },
  {
    icon: <Layers className='h-4 w-4 text-sky-600' />,
    title: '多模型接入',
    description: 'OpenAI、Claude 主流模型统一 API 调用，无需切换接口。',
  },
]

const usagePaths = [
  {
    step: '1',
    icon: <Wallet className='h-4 w-4 text-emerald-600' />,
    title: '充值或选套餐',
    description: '按需选择 GPT 套餐，或单独充值通用额度。',
  },
  {
    step: '2',
    icon: <MousePointerClick className='h-4 w-4 text-sky-600' />,
    title: '统一调用模型',
    description: '一套 API 接入主流模型，盲盒、订阅与余额的扣费顺序自己可调。',
  },
  {
    step: '3',
    icon: <Gift className='h-4 w-4 text-amber-600' />,
    title: '领取活动福利',
    description: '开盲盒抽随机奖励、用积分兑权益、邀请好友得额度刷新。',
  },
]

export function Hero(props: HeroProps) {
  const shouldReduce = useReducedMotion()
  const initial = shouldReduce ? false : 'initial'

  return (
    <section className='relative flex min-h-screen items-center overflow-hidden px-6 pt-28 pb-20 md:px-10 md:pt-32 md:pb-24'>
      <div
        aria-hidden
        className='absolute inset-0'
        style={{
          background:
            'radial-gradient(circle at 12% 14%, rgba(58,112,199,0.22), transparent 30%), radial-gradient(circle at 88% 12%, rgba(240,138,88,0.2), transparent 28%), radial-gradient(circle at 50% 96%, rgba(94,162,240,0.16), transparent 32%), linear-gradient(180deg, rgba(241,245,251,1), rgba(255,255,255,0.94))',
        }}
      />
      <div
        aria-hidden
        className='to-background pointer-events-none absolute inset-x-0 bottom-0 h-32 bg-gradient-to-b from-transparent'
      />

      <div className='relative mx-auto w-full max-w-7xl'>
        <div className='grid items-center gap-10 lg:grid-cols-[minmax(0,1.05fr)_minmax(400px,0.95fr)]'>
          <motion.div
            className='max-w-2xl'
            variants={heroStagger}
            initial={initial}
            animate='animate'
          >
            <motion.div
              variants={heroItem}
              className='ios-pill text-primary inline-flex items-center gap-2 px-3 py-1 text-xs font-semibold'
            >
              <Layers className='h-3.5 w-3.5' />
              Code Go · 多模型 AI 接入平台
            </motion.div>
            <motion.h1
              variants={heroItem}
              className='text-foreground mt-5 text-[clamp(2.4rem,4.8vw,4rem)] leading-[1.08] font-semibold tracking-[-0.03em] text-balance'
            >
              一个 API，
              <br className='hidden sm:block' />
              接入主流大模型
            </motion.h1>
            <motion.p
              variants={heroItem}
              className='text-muted-foreground mt-5 max-w-xl text-lg leading-8'
            >
              面向开发者的多模型 AI
              接入平台。盲盒开奖励、积分兑权益、邀请换刷新，多重活动福利让你的额度越用越多。
            </motion.p>

            <motion.div variants={heroItem} className='mt-7'>
              <div className='text-muted-foreground text-xs font-medium tracking-[0.16em]'>
                已接入主流模型
              </div>
              <div className='mt-3 flex flex-wrap items-center gap-2.5'>
                {models.map((model) => (
                  <div
                    key={model.key}
                    className='ios-pill flex items-center gap-2 px-3 py-1.5'
                  >
                    {getLobeIcon(model.key, 18)}
                    <span className='text-foreground text-xs font-medium'>
                      {model.label}
                    </span>
                  </div>
                ))}
              </div>
            </motion.div>

            <motion.div
              variants={heroItem}
              className='mt-8 flex flex-wrap items-center gap-3'
            >
              {props.isAuthenticated ? (
                <>
                  <Button
                    className='group rounded-full px-5'
                    render={<Link to='/dashboard' />}
                  >
                    进入控制台
                    <ArrowRight className='ml-1 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
                  </Button>
                  <Button
                    variant='outline'
                    className='rounded-full px-5'
                    render={<a href='/pricing' />}
                  >
                    查看模型
                  </Button>
                </>
              ) : (
                <>
                  <Button
                    className='group rounded-full px-5'
                    render={<Link to='/sign-up' />}
                  >
                    立即开始
                    <ArrowRight className='ml-1 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
                  </Button>
                  <Button
                    variant='outline'
                    className='rounded-full px-5'
                    render={<Link to='/pricing' />}
                  >
                    查看定价
                  </Button>
                </>
              )}
            </motion.div>
          </motion.div>

          <motion.div
            className='ios-floating-shell overflow-hidden p-6 md:p-7'
            variants={heroShell}
            initial={initial}
            animate='animate'
          >
            <div className='flex items-center justify-between'>
              <div>
                <div className='text-foreground text-sm font-semibold'>
                  三步开始使用
                </div>
                <div className='text-muted-foreground mt-1 text-xs leading-5'>
                  从充值到领取福利，整个流程都很直接。
                </div>
              </div>
              <span className='ios-pill text-primary px-2.5 py-1 text-[11px] font-medium'>
                Code Go
              </span>
            </div>

            <motion.div
              className='mt-5 space-y-3'
              variants={shellList}
              initial={initial}
              animate='animate'
            >
              {usagePaths.map((path) => (
                <motion.div
                  key={path.step}
                  variants={shellItem}
                  className='ios-pill flex items-start gap-3 p-4'
                >
                  <span className='bg-primary/12 text-primary flex size-8 shrink-0 items-center justify-center rounded-full text-sm font-semibold'>
                    {path.step}
                  </span>
                  <div className='min-w-0'>
                    <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
                      {path.icon}
                      {path.title}
                    </div>
                    <div className='text-muted-foreground mt-1 text-sm leading-6'>
                      {path.description}
                    </div>
                  </div>
                </motion.div>
              ))}
            </motion.div>
          </motion.div>
        </div>

        <motion.div
          className='mt-12 grid gap-4 sm:grid-cols-3'
          variants={heroStagger}
          initial={initial}
          animate='animate'
        >
          {advantages.map((item) => (
            <HeroFact
              key={item.title}
              icon={item.icon}
              title={item.title}
              description={item.description}
            />
          ))}
        </motion.div>
      </div>
    </section>
  )
}

function HeroFact(props: {
  icon: ReactNode
  title: string
  description: string
}) {
  return (
    <motion.div variants={heroItem} className='ios-floating-shell h-full p-5'>
      <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
        {props.icon}
        {props.title}
      </div>
      <div className='text-muted-foreground mt-1.5 text-sm leading-6'>
        {props.description}
      </div>
    </motion.div>
  )
}
