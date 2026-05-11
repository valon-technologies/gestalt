# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.24"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.24/gestaltd-macos-arm64.tar.gz"
      sha256 "c65a69c92fc915c06a7bb47023aed355429cda2a0da22c56713be169f9bbc23a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.24/gestaltd-macos-x86_64.tar.gz"
      sha256 "57d9fc2a635640cca1d2758e0d07520b30d21862742c3a1b054d616802fc2bcb"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.24/gestaltd-linux-arm64.tar.gz"
      sha256 "b61d1d73f4adcdf36d38f3a254673b5ba3eefe98f9bbba787255ffbf7c231de8"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.24/gestaltd-linux-x86_64.tar.gz"
      sha256 "af2ef9d7b502313a2487c9b715c9048f1b0f2ca235398150482d7f2b45c76309"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
