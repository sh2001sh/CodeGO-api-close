import { useState } from 'react'
import { Percent } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { createMarketplaceBargainRequest } from '../api'
import type { MarketplaceGroup } from '../types'

export function BargainProposalDialog(props: { group: MarketplaceGroup; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useTranslation(); const [multiplier, setMultiplier] = useState(''); const [reason, setReason] = useState(''); const [saving, setSaving] = useState(false)
  const submit = async () => { const value = Number(multiplier); if (!(value > 0)) return; setSaving(true); try { await createMarketplaceBargainRequest({ groupId: props.group.id, proposedMultiplier: value, reason: reason.trim() }); toast.success(t('倍率议价申请已提交')); props.onOpenChange(false); setMultiplier(''); setReason('') } catch (error) { toast.error(error instanceof Error ? error.message : t('提交失败')) } finally { setSaving(false) } }
  return <Dialog open={props.open} onOpenChange={props.onOpenChange}><DialogContent><DialogHeader><DialogTitle className='flex items-center gap-2'><Percent />{t('申请倍率议价')}</DialogTitle><DialogDescription>{props.group.system_display_name} · {t('当前倍率')} {props.group.multiplier}x</DialogDescription></DialogHeader><div className='space-y-3'><div className='space-y-2'><Label>{t('期望倍率')}</Label><Input type='number' min={0.01} step={0.01} value={multiplier} onChange={(event) => setMultiplier(event.target.value)} /></div><div className='space-y-2'><Label>{t('申请说明')}</Label><Textarea value={reason} onChange={(event) => setReason(event.target.value)} /></div></div><DialogFooter><Button variant='outline' onClick={() => props.onOpenChange(false)}>{t('Cancel')}</Button><Button disabled={saving || Number(multiplier) <= 0} onClick={() => void submit()}>{t('提交申请')}</Button></DialogFooter></DialogContent></Dialog>
}
