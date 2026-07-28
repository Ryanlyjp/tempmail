import importlib.util
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("mail-receiver.py")
SPEC = importlib.util.spec_from_file_location("mail_receiver", MODULE_PATH)
MAIL_RECEIVER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MAIL_RECEIVER)


class ParseEmailTest(unittest.TestCase):
    def test_parses_utf8_8bit_multipart_body_without_unicode_escapes(self):
        boundary = "microsoft-boundary"
        text_body = "请对个人 Microsoft 帐户使用以下安全代码。\r\n\r\n安全代码: 206380"
        html_body = "<html><body><p>安全代码</p><strong>206380</strong></body></html>"
        raw = (
            "From: Microsoft Account Team <account-security-noreply@accountprotection.microsoft.com>\r\n"
            "Subject: Microsoft security code\r\n"
            "MIME-Version: 1.0\r\n"
            f'Content-Type: multipart/alternative; boundary="{boundary}"\r\n'
            "\r\n"
            f"--{boundary}\r\n"
            "Content-Type: text/plain; charset=utf-8\r\n"
            "Content-Transfer-Encoding: 8bit\r\n"
            "\r\n"
            f"{text_body}\r\n"
            f"--{boundary}\r\n"
            "Content-Type: text/html; charset=utf-8\r\n"
            "Content-Transfer-Encoding: 8bit\r\n"
            "\r\n"
            f"{html_body}\r\n"
            f"--{boundary}--\r\n"
        ).encode("utf-8")

        _, _, parsed_text, parsed_html = MAIL_RECEIVER.parse_email(raw)

        self.assertEqual(text_body, parsed_text.rstrip("\r\n"))
        self.assertEqual(html_body, parsed_html.rstrip("\r\n"))
        self.assertNotIn("\\u", parsed_text)
        self.assertNotIn("\\u", parsed_html)


if __name__ == "__main__":
    unittest.main()
