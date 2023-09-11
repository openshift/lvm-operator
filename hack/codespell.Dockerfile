FROM python:3-alpine

RUN pip install codespell

WORKDIR /repo

CMD ["codespell"]