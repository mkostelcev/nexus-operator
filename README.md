# Sonatype Nexus Operator

[![Go Report Card](https://goreportcard.com/badge/github.com/mkostelcev/nexus-operator)](https://goreportcard.com/report/github.com/mkostelcev/nexus-operator)

Kubernetes Operator для автоматизации управления экземпляром **Nexus Repository Manager**.  
Оператор упрощает настройку и обслуживание Nexus в Kubernetes-кластере.
Поддерживает управление сущностями: **Role**, **Privilege**, **ContentSelector**, **Repository**

## 📦 Установка

### Требования

- Kubernetes 1.20+

### Запуск оператора локально в режиме разработки

- Переключите kube-context на нужный вам кластер и namespace, где будет находиться оператор
- Задайте ENV-переменные:
  - `NEXUS_URL` - адрес Nexus, которым вы хотите управлять
  - `NEXUS_USER` - пользователь Nexus, из под которого будут совершаться операции в его API
  - `NEXUS_PASSWORD` - пароль данного пользователя
- Выполните `make install` - данной командой вы установите CRD в кластер (пространство: nexus.operators.dev.kostoed.ru)
- Выполните `make run` - и вы запустите оператор локально

⚠️ Обратите внимание: пробы (liveness и readiness) находятся на порту `8080`, а метрики - на порту `8081`.

🤝 Участие в разработке
PR и issues приветствуются!
Перед началом:

- Форкните репозиторий
- Установите зависимости: `make setup`

⚠️ Предупреждение:
Этот проект находится в стадии альфа-тестирования. Не используйте в production без дополнительной проверки.
