import yaml

SB = '/home/kara/.hermes/cache/scratch/r8'
d = yaml.safe_load(open(f'{SB}/inventory.yaml').read())
s = d['components']['schemas']
s['Item'].pop('required', None)
s['Item']['properties']['sku']['example'] = 'A-100'
s['Item']['properties']['qty']['example'] = 12
s['Item']['properties']['qty']['description'] = 'units on hand'
s['Item']['properties']['tags']['items'] = {'type': 'string'}
s['Item']['properties']['discontinued'] = {'type': 'boolean'}
s['NewItem']['properties']['qty']['default'] = 1
s['StockReport'].pop('example', None)
for k in ('/items', '/items/{sku}', '/stock-levels'):
    for m, ops in d['paths'][k].items():
        if not isinstance(ops, dict):
            continue
        for code, r in ops.get('responses', {}).items():
            if not isinstance(r.get('description'), str):
                r['description'] = 'ok'
open(f'{SB}/clean.yaml', 'w').write(yaml.safe_dump(d, sort_keys=False))
print('wrote clean.yaml')
