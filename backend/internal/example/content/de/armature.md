---
labels: anleitung, anbindungen
---
# Armature

%%properties%%

| Zielgruppe | Alle |
|---|---|
| Lesezeit | 3 Minuten |

%%end%%

Stator arbeitet Hand in Hand mit Armature, wo Teams ihre Arbeit als Vorgänge planen und verfolgen. Beide melden Personen über denselben Identitätsanbieter an, so ist eine Person in beiden dieselbe.

## Verbinden

1. Administratoren der Organisation verbinden sie unter **Armature** im Kontomenü, mit der Adresse von Armature und der Organisation dort.
2. Alle erstellen in Armature ein Token, das nicht nur lesen darf, und fügen es unter **Armature-Token** in ihrem Profil ein.

Jede Frage an Armature wird als die lesende Person gestellt, so zeigt eine Seite nie einen Vorgang, den die Lesenden in Armature nicht öffnen könnten.

## Vorgänge in Seiten

- Ein getippter Vorgangsschlüssel wie CP-12 mit einem Leerzeichen oder Satzzeichen danach wird zum Chip mit dem Status des Vorgangs; ebenso die eingefügte Adresse eines Vorgangs.
- Das Menü bietet eine Karte **Armature-Vorgang**, eine **Armature-Vorgangsliste** aus einer Abfrage, ein **Armature-Diagramm** und eine **Armature-Roadmap**; ein **Kalender** zeigt auch die fälligen Vorgänge eines Projekts.
- Markieren Sie Text oder Listeneinträge im Editor, und **Armature-Vorgang anlegen** legt sie einzeln als Vorgänge an und setzt ihre Chips in den Text.

{{if .Armature}}Jeder davon steht in [Alle Blöcke einer Seite](showcase.md), aus dem Projekt {{.Armature.Project}}.{{else}}Diese Organisation hat kein Armature, das der Person, die diesen Bereich angelegt hat, ein Projekt gezeigt hat, deshalb sagt [Alle Blöcke einer Seite](showcase.md) das an Stelle der Armature-Blöcke.{{end}}

## Zurück in Armature

Wird eine Seite veröffentlicht, die einen Vorgang nennt, listet der Vorgang die Seite in Armature. Die Seite zeigt unter **In Armature verknüpft**, ob jede Verknüpfung steht. Ihrem Armature-Design zu folgen, wählen Sie unter **Designs** im Kontomenü.
