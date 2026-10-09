"""Focused inventory pagination checks; no processes or fixture secrets."""
import unittest
from unittest.mock import patch
import drill
from common import CheckFailed


class CredentialPaginationTests(unittest.TestCase):
    def test_collects_more_than_default_page_at_one_revision(self):
        items = [{'generation': 1, 'app_id': 'fixture-' + str(i)} for i in range(205)]
        def response(_hub, _state, operation, payload=None):
            if operation == 'recovery.status':
                return {'revision': 7}
            offset = payload['offset']
            return {'revision': 7, 'offset': offset, 'limit': 100,
                    'total': len(items), 'statuses': items[offset:offset + 100]}
        with patch.object(drill, 'request', side_effect=response) as call:
            self.assertEqual(drill.credential_pages(None, None, 7), items)
            self.assertEqual([c.args[3]['offset'] for c in call.call_args_list[:-1]], [0, 100, 200])

    def test_rejects_changed_revision_in_page_or_after_collection(self):
        for changed in ('page', 'final'):
            def response(_hub, _state, operation, payload=None):
                if operation == 'recovery.status':
                    return {'revision': 8 if changed == 'final' else 7}
                return {'revision': 8 if changed == 'page' else 7, 'offset': 0,
                        'limit': 100, 'total': 1, 'statuses': [{'generation': 1}]}
            with patch.object(drill, 'request', side_effect=response), self.assertRaises(CheckFailed):
                drill.credential_pages(None, None, 7)

    def test_rejects_truncated_inventory(self):
        with patch.object(drill, 'request', return_value={'revision': 7, 'offset': 0,
                'limit': 100, 'total': 101, 'statuses': []}), self.assertRaises(CheckFailed):
            drill.credential_pages(None, None, 7)


if __name__ == '__main__':
    unittest.main()
