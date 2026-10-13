
- source_spec: `spec-callback-installer.md`
  summary: 为既有callback加密Token提供只读存储一致性诊断，识别错误32字节key或损坏密文。
  evidence: baseline的NewEncryptedFileTokenSink只加载key，密文读取发生在Save；HTTP健康检查只证明服务可用，不能证明旧Token可解密。本轮保持该行为并明确实机与存量数据验收边界。
