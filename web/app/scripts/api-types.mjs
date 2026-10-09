import openapiTS, { astToString, COMMENT_HEADER } from 'openapi-typescript'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

function integerFields(schema, document, prefix = [], refs = new Set()) {
  if (!schema) return []
  if (schema.$ref) {
    if (refs.has(schema.$ref)) return []
    const value = schema.$ref
      .split('/')
      .slice(1)
      .reduce((node, key) => node[key], document)
    return integerFields(value, document, prefix, new Set([...refs, schema.$ref]))
  }
  if (schema.type === 'integer' && schema.format === 'int64') return [prefix]
  if (schema.items) return integerFields(schema.items, document, [...prefix, '*'], refs)
  return [
    ...Object.entries(schema.properties ?? {}).flatMap(([key, value]) =>
      integerFields(value, document, [...prefix, key], refs),
    ),
    ...(typeof schema.additionalProperties === 'object'
      ? integerFields(schema.additionalProperties, document, [...prefix, '*'], refs)
      : []),
    ...(schema.allOf ?? []).flatMap((value) => integerFields(value, document, prefix, refs)),
  ]
}

export async function generateTypes() {
  const source = new URL('../../../v3/api/openapi.json', import.meta.url)
  const document = JSON.parse(await readFile(source, 'utf8'))
  const nodes = await openapiTS(source, {
    transform(schema) {
      if (
        (schema.type === 'integer' && schema.format === 'int64') ||
        schema['x-codego-exact-decimal'] === true
      ) {
        // Exact JSON decimals/large integers are read as strings; requests accept bigint.
        const types = [
          ts.factory.createKeywordTypeNode(ts.SyntaxKind.NumberKeyword),
          ts.factory.createKeywordTypeNode(ts.SyntaxKind.StringKeyword),
          ts.factory.createKeywordTypeNode(ts.SyntaxKind.BigIntKeyword),
        ]
        if (schema.nullable) types.push(ts.factory.createLiteralTypeNode(ts.factory.createNull()))
        return ts.factory.createUnionTypeNode(types)
      }
    },
  })
  const fields = {}
  for (const [path, methods] of Object.entries(document.paths)) {
    for (const [method, operation] of Object.entries(methods)) {
      const list = integerFields(
        operation.requestBody?.content?.['application/json']?.schema,
        document,
      )
      if (list.length) fields[`${method.toUpperCase()}:${path}`] = list
    }
  }
  return (
    COMMENT_HEADER +
    astToString(nodes) +
    `\nexport const integerRequestFields: Readonly<Record<string, readonly (readonly string[])[]>> = ${JSON.stringify(fields, null, 2)};\n`
  )
}
