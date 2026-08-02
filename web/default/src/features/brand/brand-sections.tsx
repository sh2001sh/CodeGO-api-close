import { Link } from '@tanstack/react-router'
import {
  ArrowUpRight,
  CircleDot,
  Gift,
  KeyRound,
  MonitorUp,
  Wallet,
} from 'lucide-react'
import { Button } from '@/components/ui/button'

export function OffersSection() {
  return (
    <section id='offers' className='offer-story scroll-mt-16'>
      <div className='offer-story-heading'>
        <span>02 / 破晓</span>
        <h2>套餐与盲盒</h2>
        <p>稳定的长期额度，和偶尔发生的惊喜。</p>
      </div>

      <article className='offer-story-row offer-story-package'>
        <div className='offer-story-index'>MONTH / DAY</div>
        <h3>套餐</h3>
        <div className='offer-story-detail'>
          <Wallet className='size-5' />
          <p>
            月卡适合持续开发，日卡适合集中调用。人民币支付，额度与使用情况随时可查。
          </p>
          <Button render={<Link to='/packages' />}>
            查看套餐
            <ArrowUpRight className='size-4' />
          </Button>
        </div>
      </article>

      <article className='offer-story-row offer-story-box'>
        <div className='offer-story-index'>RANDOM / REWARD</div>
        <h3>盲盒</h3>
        <div className='offer-story-detail'>
          <Gift className='size-5' />
          <p>
            随机获得普通额度、Claude
            专用额度或其他权益，为下一次调用补充一点意外。
          </p>
          <Button variant='outline' render={<Link to='/blind-box' />}>
            去开盲盒
            <ArrowUpRight className='size-4' />
          </Button>
        </div>
      </article>
    </section>
  )
}

const facts = [
  {
    icon: KeyRound,
    title: '统一密钥',
    text: '一套凭据连接 Codex、Claude Code 与更多模型。',
  },
  {
    icon: MonitorUp,
    title: '桌面协同',
    text: '网页管理后，一键应用到本地 AI 编程工具。',
  },
  {
    icon: CircleDot,
    title: '清晰用量',
    text: '额度、日志、套餐和钱包余额集中呈现。',
  },
]

export function SiteOverviewSection() {
  return (
    <section id='about' className='site-orbit scroll-mt-16'>
      <div className='site-orbit-ring' aria-hidden />
      <div className='site-orbit-copy'>
        <span>03 / 日出</span>
        <h2>
          一个长期可用的
          <br />
          AI 编程入口
        </h2>
        <p>减少重复配置和供应商切换，让注意力回到开发本身。</p>
      </div>
      <div className='site-orbit-facts'>
        {facts.map((fact) => (
          <article key={fact.title}>
            <fact.icon className='size-5' />
            <h3>{fact.title}</h3>
            <p>{fact.text}</p>
          </article>
        ))}
      </div>
    </section>
  )
}
