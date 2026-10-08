// "获取模型" action inside the channel form. For an existing channel it
// probes the channel's stored credential via GET /api/channel/fetch_models/{id}.
// For a brand-new channel (not saved yet) it probes the in-form base_url/key
// via POST /api/channel/fetch_models, which requires a legacy numeric
// provider type (see legacyProviderType in ./types).
import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { useTranslation } from '../../lib/i18n'
import { Button, Callout } from '../../components/ui'
import { legacyProviderType } from './types'

export function FetchModelsAction(props: {
  channelID?: string
  provider: string
  baseURL: string
  secret: string
  selected: string[]
  onPick: (models: string[]) => void
}) {
  const { t } = useTranslation()
  const [picked, setPicked] = useState<string[]>([])
  const fetchModels = useMutation({
    mutationFn: () => {
      if (props.channelID)
        return api
          .GET('/api/channel/fetch_models/{id}', { params: { path: { id: props.channelID } } })
          .then((result) => unwrap(result))
      const type = legacyProviderType(props.provider)
      if (type === undefined) throw new Error('该 Provider 不支持获取模型')
      if (!props.secret) throw new Error('请先填写凭据')
      return api
        .POST('/api/channel/fetch_models', {
          body: { base_url: props.baseURL, type, key: props.secret },
        })
        .then((result) => unwrap(result))
    },
    onSuccess: (models) => setPicked(models),
  })
  return (
    <div className="form-panel full-width" style={{ flexBasis: '100%' }}>
      <Button
        type="button"
        variant="quiet"
        disabled={fetchModels.isPending}
        onClick={() => fetchModels.mutate()}
      >
        {t('获取模型')}
      </Button>
      {fetchModels.error && <Callout tone="danger">{t(fetchModels.error.message)}</Callout>}
      {picked.length > 0 && (
        <fieldset className="full-width" style={{ flexBasis: '100%' }}>
          <legend>{t('从上游选择模型')}</legend>
          <div className="row-actions" role="group" aria-label={t('上游模型')}>
            {picked.map((model) => {
              const checked = props.selected.includes(model)
              return (
                <label key={model} className="checkbox-field">
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() =>
                      props.onPick(
                        checked
                          ? props.selected.filter((item) => item !== model)
                          : [...props.selected, model],
                      )
                    }
                  />
                  {model}
                </label>
              )
            })}
          </div>
        </fieldset>
      )}
    </div>
  )
}
