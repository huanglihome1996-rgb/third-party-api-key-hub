"""Generate and validate the fixture v1 request schema from Go DTOs.

Requires jsonschema only for validation (uv run --with jsonschema ...).
The Go contract remains the sole owner of public field names and types.
"""
import json
import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[1]
DEST = ROOT / '_bmad-output/planning-artifacts/contracts/cli-v1'
SOURCE = (ROOT / 'internal/contract/v1/contract.go').read_text(encoding='utf-8')
ID = {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[^\s|\x00-\x1f\x7f]+$'}

def definition(name):
    body = re.search(r'type ' + name + r' struct \{(.*?)\n\}', SOURCE, re.S).group(1)
    properties = {}
    for _, typ, key in re.findall(r'(\w+)\s+(\*?string|bool|int64|int)\s+`json:"([^",]+)(?:,[^"]*)?"`', body):
        value = {'type': {'string': 'string', 'bool': 'boolean', 'int64': 'integer', 'int': 'integer', '*string': ['string', 'null']}[typ]}
        if key.endswith('_id') or key == 'type':
            value = dict(ID)
        if key == 'platform':
            value = {'const': 'fixture'}
        if key == 'name':
            value['maxLength'] = 256
        if key == 'value':
            value.update(minLength=1, maxLength=65536)
        if key in ('expected_revision', 'expected_generation'):
            value['minimum'] = 1
        if key == 'offset':
            value['minimum'] = 0
        if key == 'limit':
            value.update(minimum=1, maximum=1000)
        properties[key] = value
    return {'type': 'object', 'additionalProperties': False, 'properties': properties}

OPERATIONS = {
    'app.register': ('RegisterAppInput', ['app_id','platform','provider_app_id']),
    'shop.register': ('RegisterShopInput', ['external_shop_id','platform','provider_shop_id']),
    'brand.register': ('RegisterBrandInput', ['brand_id']),
    'internal_shop.register': ('RegisterInternalShopInput', ['internal_shop_id']),
    'shop.map_brand': ('MapInput', ['external_shop_id','brand_id','expected_revision']),
    'shop.unmap_brand': ('MapInput', ['external_shop_id','expected_revision']),
    'brand.map_internal_shop': ('MapInput', ['brand_id','internal_shop_id','expected_revision']),
    'brand.unmap_internal_shop': ('MapInput', ['brand_id','expected_revision']),
    'installation.create': ('InstallationInput', ['app_id','external_shop_id','expected_revision']),
    'credential.import': ('ImportCredentialInput', ['scope','app_id','type','value','replace','expiry_known','expires_at','confirm']),
    'credential.query': ('QueryInput', ['by']),
    'credential.confirm': ('CredentialRefInput', ['scope','app_id','type','expected_generation']),
    'credential.mark_invalid': ('CredentialRefInput', ['scope','app_id','type','expected_generation','reason']),
    'credential.disable': ('CredentialRefInput', ['scope','app_id','type']),
    'credential.restore': ('CredentialRefInput', ['scope','app_id','type','expected_generation']),
    'credential.status': ('ListInput', []),
    'installation.list': ('ListInput', []),
    'audit.list': ('AuditListInput', []),
}
for op in ['app.list','shop.list','brand.list','internal_shop.list']:
    OPERATIONS[op] = ('ListInput', [])
OPERATIONS['health'] = (None, [])

branches = []
for op, (dto, required) in sorted(OPERATIONS.items()):
    payload = definition(dto) if dto else {'type': 'object', 'properties': {}, 'additionalProperties': False}
    payload['required'] = required
    if dto == 'ListInput' and op not in ('installation.list', 'credential.status'):
        payload['properties'] = {key:value for key,value in payload['properties'].items() if key in ('offset','limit')}
    if dto == 'MapInput':
        payload['properties'] = {key: value for key, value in payload['properties'].items() if key in required}
    if dto == 'CredentialRefInput':
        payload['properties'] = {key: value for key,value in payload['properties'].items() if key != 'reason' or op == 'credential.mark_invalid'}
        if op == 'credential.disable':
            payload['properties'].pop('expected_generation', None)
        if op == 'credential.mark_invalid':
            payload['properties']['reason'] = {'enum': ['rejected','revoked','invalid']}
    if dto in ('CredentialRefInput', 'ImportCredentialInput'):
        payload['properties']['scope'] = {'enum': ['app', 'shop']}
        payload['properties']['type'] = {'enum': ['client_secret','shop_token','refresh_token']}
    if dto == 'QueryInput':
        payload['properties']['by'] = {'enum': ['brand','internal_shop','external_shop']}
    if dto == 'ImportCredentialInput':
        payload['allOf'] = [
            {'if': {'properties': {'replace': {'const': True}}}, 'then': {'required': ['expected_generation']}, 'else': {'not': {'required': ['expected_generation']}}},
            {'if': {'properties': {'expiry_known': {'const': False}}}, 'then': {'properties': {'expires_at': {'type': 'null'}}}, 'else': {'properties': {'expires_at': {'type': 'string', 'pattern': 'Z$'}}}},
        ]
    if dto in ('CredentialRefInput','ImportCredentialInput'):
        payload.setdefault('allOf', []).append({'if': {'properties': {'scope': {'const': 'shop'}}}, 'then': {'required': ['external_shop_id']}, 'else': {'not': {'required': ['external_shop_id']}}})
    if dto == 'QueryInput':
        payload['allOf'] = [
            {'if': {'properties': {'by': {'const': 'external_shop'}}}, 'then': {'required': ['external_shop_id'], 'not': {'anyOf': [{'required': ['brand_id']}, {'required': ['internal_shop_id']}]}}},
            {'if': {'properties': {'by': {'const': 'brand'}}}, 'then': {'required': ['brand_id'], 'not': {'required': ['internal_shop_id']}}},
            {'if': {'properties': {'by': {'const': 'internal_shop'}}}, 'then': {'required': ['internal_shop_id'], 'not': {'required': ['brand_id']}}},
        ]
    branches.append({'type': 'object', 'additionalProperties': False, 'required': ['version','operation','payload'], 'properties': {'version': {'const': 'contract/v1'}, 'operation': {'const': op}, 'payload': payload}})

schema = {'$schema': 'https://json-schema.org/draft/2020-12/schema', '$id': 'urn:api-key-hub:fixture-v1:request', 'oneOf': branches}
DEST.mkdir(parents=True, exist_ok=True)
(DEST / 'request.schema.json').write_text(json.dumps(schema,ensure_ascii=False,indent=2) + '\n',encoding='utf-8')

def obj(props, required=None, extend=False):
    return {'type':'object','properties':props,'required':list(props) if required is None else required,'additionalProperties':extend}

codes = re.findall(r'ErrorCode\s*=\s*"([a-z_]+)"', SOURCE)
error_schema = obj({'error':{'enum':codes},'message':{'type':'string','minLength':1}})
def credential(typ):
    return {'type':'array','minItems':1,'maxItems':1,'items':obj({'type':{'const':typ},'value':{'type':'string','minLength':1}})}
def statuses(typ):
    fields = {'type':{'const':typ},'generation':{'type':'integer','minimum':1},'row_revision':{'type':'integer','minimum':1},'available':{'const':True},'disabled':{'const':False},'confirmed_by_operator':{'type':'boolean'},'expiry_known':{'type':'boolean'},'expires_at':{'type':['string','null']}}
    return {'type':'array','minItems':1,'maxItems':1,'items':obj(fields, ['type','generation','available','expiry_known','expires_at'], True)}
query_schema = obj({'identity':obj({key:ID for key in ['app_id','platform','provider_app_id','external_shop_id','provider_shop_id','brand_id','internal_shop_id']},extend=True), 'credentials':obj({'app':credential('client_secret'),'shop':credential('shop_token')}), 'status':obj({'app':statuses('client_secret'),'shop':statuses('shop_token')},extend=True)})
mutation_schema = obj({'operation_id':{'type':'string','minLength':1},'revision':{'type':'integer','minimum':1},'generation':{'type':'integer','minimum':1},'row_revision':{'type':'integer','minimum':1}},['operation_id','revision','row_revision'])
for name, output in [('error',error_schema),('query',query_schema),('mutation',mutation_schema)]:
    output = {'$schema':schema['$schema'], **output}
    (DEST / (name+'.schema.json')).write_text(json.dumps(output,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')

if __name__ == '__main__':
    from jsonschema import Draft202012Validator
    Draft202012Validator.check_schema(schema)
    validator = Draft202012Validator(schema)
    examples=[]
    for branch in branches:
        op=branch['properties']['operation']['const']
        required=branch['properties']['payload']['required']
        values={'platform':'fixture','scope':'app','type':'client_secret','value':'fixture-value','reason':'invalid','by':'external_shop','replace':False,'confirm':False,'expiry_known':False,'expires_at':None,'expected_revision':1,'expected_generation':1}
        payload={key:values.get(key,'fixture-'+key.replace('_','-')) for key in required}
        if op=='credential.query':payload['external_shop_id']='fixture-shop'
        valid={'version':'contract/v1','operation':op,'payload':payload}
        invalid={**valid,'payload':{**payload,'unknown_fixture_field':'fixture-value'}}
        assert validator.is_valid(valid), op
        assert not validator.is_valid(invalid), op
        examples.append({'operation':op,'valid_request':valid,'invalid_request':invalid})
    for output in [error_schema,query_schema,mutation_schema]:Draft202012Validator.check_schema(output)
    regressions = [
        ('app.list', {'limit':0}), ('app.list', {'limit':1001}),
        ('app.list', {'app_id':''}), ('credential.status', {'app_id':''}),
        ('credential.import', {'scope':'app','app_id':'a','type':'client_secret','value':'fixture-v','replace':False,'expected_generation':0,'expiry_known':False,'expires_at':None,'confirm':True}),
        ('credential.import', {'scope':'app','app_id':'a','type':'client_secret','value':'fixture-v','replace':False,'external_shop_id':'s','expiry_known':False,'expires_at':None,'confirm':True}),
    ]
    for op, payload in regressions:
        assert not validator.is_valid({'version':'contract/v1','operation':op,'payload':payload}), (op,payload)
    assert validator.is_valid({'version':'contract/v1','operation':'app.list','payload':{'offset':0,'limit':1000}})
    fixture = {'operations':examples}
    (DEST/'examples.json').write_text(json.dumps(fixture,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    result = {'status':'PASS','operations':len(OPERATIONS),'example_checks':len(examples)*2,'regression_checks':len(regressions)+1,'source':'internal/contract/v1/contract.go','scope':'static fixture schema; domain rules/duplicates/EOF/body limits require Go tests'}
    (DEST/'validation.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(result))
