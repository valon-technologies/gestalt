# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.22"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.22/gestaltd-macos-arm64.tar.gz"
      sha256 "1724d15186726a6bad94430fe4097240f885064358722893fdfcc1b5974bfd9e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.22/gestaltd-macos-x86_64.tar.gz"
      sha256 "e5a184b4b5896dc73b32060642b9f719cbec01d4e055691f65ae573b64750132"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.22/gestaltd-linux-arm64.tar.gz"
      sha256 "30db6269e425ed9b046b75c9297ec031000c55de9ea1a6518bb675b7235a2a81"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.22/gestaltd-linux-x86_64.tar.gz"
      sha256 "1fea1cc695df6df61e970e5fccf515d0646ed7d8ea474e979587dc1d27430cac"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
