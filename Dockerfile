FROM ubuntu
RUN dpkg-divert --local --rename --add /sbin/initctl
RUN ln -sf /bin/true /sbin/initctl
ENV DEBIAN_FRONTEND noninteractive
RUN apt-get update
RUN apt-get install -y unzip zip ca-certificates curl git nodejs npm  # node needed for upload-artifact