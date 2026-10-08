import { useTranslation } from '../lib/i18n'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { modelFavoritesOptions, publicModelsOptions } from '../lib/public-catalog'
import { positiveID, errorFrom } from '../features/commerce/amounts'
import { useState } from 'react'
import { Button, ErrorMessage, SelectField, Loading, PageHeader } from '../components/ui'
import { Link } from '@tanstack/react-router'
import { DataTable } from '../components/data-table'

export default function ModelFavoritesPage() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [error, setError] = useState<Error | null>(null)
  const favorites = useQuery(modelFavoritesOptions())
  const catalog = useQuery(publicModelsOptions())
  const available = (catalog.data ?? []).filter(
    (model) =>
      model.model_id &&
      !favorites.data?.model_ids.some((id) => String(id) === String(model.model_id)),
  )
  const save = useMutation({
    mutationFn: (body: { model_id: string; favorite: boolean }) =>
      api.PUT('/api/models/favorites/', { body }).then(unwrap),
    onSuccess: () => client.invalidateQueries({ queryKey: ['model-favorites'] }),
  })
  return (
    <>
      <PageHeader title="模型收藏" />
      <p>
        <Link to="/models">{t('模型与价格')}</Link>
      </p>
      <ErrorMessage error={error ?? favorites.error ?? catalog.error ?? save.error} />
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          setError(null)
          try {
            save.mutate({
              model_id: positiveID(String(new FormData(event.currentTarget).get('model_id'))),
              favorite: true,
            })
          } catch (cause) {
            setError(errorFrom(cause))
          }
        }}
      >
        <SelectField
          name="model_id"
          label="选择模型"
          required
          options={[
            { value: '', label: '请选择模型' },
            ...available.map((model) => ({ value: String(model.model_id), label: model.name })),
          ]}
          disabled={catalog.isPending || available.length === 0}
        />
        <Button
          type="submit"
          disabled={save.isPending || catalog.isPending || available.length === 0}
        >
          {t('添加收藏')}
        </Button>
      </form>
      {favorites.isPending && <Loading />}
      {favorites.data && (
        <DataTable
          rows={favorites.data.model_ids}
          rowKey={String}
          columns={[
            {
              label: '模型',
              render: (id) =>
                favorites.data?.models?.find((model) => String(model.id) === String(id))
                  ?.model_name ??
                catalog.data?.find((model) => String(model.model_id) === String(id))?.name ??
                String(id),
            },
            {
              label: '操作',
              render: (id) => (
                <Button
                  variant="quiet"
                  disabled={save.isPending}
                  onClick={() => save.mutate({ model_id: String(id), favorite: false })}
                >
                  {t('取消收藏')}
                </Button>
              ),
            },
          ]}
          empty="尚未收藏模型。"
        />
      )}
    </>
  )
}
